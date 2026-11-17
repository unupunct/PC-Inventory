// Command server runs the PC Inventory Manager central server.
package main

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"math/big"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/pcim/pc-inventory-manager/internal/server/alerts"
	"github.com/pcim/pc-inventory-manager/internal/server/api"
	"github.com/pcim/pc-inventory-manager/internal/server/auth"
	"github.com/pcim/pc-inventory-manager/internal/server/config"
	"github.com/pcim/pc-inventory-manager/internal/server/discovery"
	"github.com/pcim/pc-inventory-manager/internal/server/store"
	"github.com/pcim/pc-inventory-manager/internal/shared/models"
	"github.com/pcim/pc-inventory-manager/internal/shared/version"
)

func main() {
	var (
		envFile    = flag.String("env", "", "path to environment file (default .env, or the service configuration in service mode)")
		listen     = flag.String("listen", "", "listen address override, e.g. 0.0.0.0:8080")
		dataDir    = flag.String("data", "", "data directory override")
		showVer    = flag.Bool("version", false, "print version and exit")
		resetAdmin = flag.String("reset-admin-password", "", "reset the admin password to this value and exit")
	)
	flag.Parse()

	if *showVer {
		fmt.Printf("PC Inventory Manager server %s (protocol %d) %s/%s\n", version.Version, version.Protocol, runtime.GOOS, runtime.GOARCH)
		return
	}

	// Service management subcommands: pcim-server service install|start|stop|status|uninstall
	if flag.NArg() > 0 && flag.Arg(0) == "service" {
		svcEnv := *envFile
		if svcEnv == "" {
			svcEnv = serverConfigPath()
		}
		if err := serverServiceCommand(flag.Arg(1), svcEnv); err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			os.Exit(1)
		}
		return
	}

	envPath := *envFile
	if envPath == "" && !isServiceMode(os.Args[1:]) {
		envPath = ".env"
	}
	opts := runOptions{envFile: envPath, listen: *listen, dataDir: *dataDir, resetAdmin: *resetAdmin}

	// Under the Windows service manager there is no console: run the server in
	// the service context and keep the SCM informed of our state.
	if isServiceMode(os.Args[1:]) {
		logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
		if err := runAsService(func(ctx context.Context) error { return run(ctx, opts, true) }, logger); err != nil {
			fmt.Fprintln(os.Stderr, "service error:", err)
			os.Exit(1)
		}
		return
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, opts, false); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

type runOptions struct {
	envFile    string
	listen     string
	dataDir    string
	resetAdmin string
}

// run starts the server and blocks until ctx is cancelled or the listener
// fails. Errors are returned instead of calling os.Exit so that the same code
// path serves the console and the Windows service.
func run(ctx context.Context, opts runOptions, asService bool) error {
	cfg, err := config.Load(opts.envFile)
	if err != nil {
		return fmt.Errorf("configuration error: %w", err)
	}
	// A service starts with no working directory and no environment, so use
	// machine-wide locations unless they were given explicitly.
	if asService {
		if opts.dataDir == "" && os.Getenv("PCIM_DATA_DIR") == "" && os.Getenv("DATA_DIR") == "" {
			opts.dataDir = serverDataDir()
		}
		if cfg.LogFile == "" {
			cfg.LogFile = filepath.Join(opts.dataDir, "server.log")
		}
	}
	if opts.dataDir != "" {
		cfg.DataDir = opts.dataDir
		cfg.DBPath = filepath.Join(opts.dataDir, "pcim.db")
		cfg.BackupDir = filepath.Join(opts.dataDir, "backups")
	}
	if opts.listen != "" {
		host, port, splitErr := net.SplitHostPort(opts.listen)
		if splitErr != nil {
			return fmt.Errorf("invalid -listen value %q", opts.listen)
		}
		cfg.ListenHost = host
		if p, err := strconv.Atoi(port); err == nil {
			cfg.Port = p
		}
	}

	logger := newLogger(cfg)
	slog.SetDefault(logger)

	st, err := store.Open(cfg.DBPath)
	if err != nil {
		return fmt.Errorf("cannot open database at %s: %w", cfg.DBPath, err)
	}
	defer st.Close()

	if opts.resetAdmin != "" {
		hash, err := auth.HashPassword(opts.resetAdmin)
		if err != nil {
			return fmt.Errorf("password too short (min 8 chars): %w", err)
		}
		admin, err := st.GetAdminByUsername(ctx, cfg.AdminUser)
		if err != nil {
			id, err := st.CreateAdmin(ctx, cfg.AdminUser, hash)
			if err != nil {
				return fmt.Errorf("create admin failed: %w", err)
			}
			logger.Info("created admin", "id", id, "user", cfg.AdminUser)
		} else if err := st.UpdateAdminPassword(ctx, admin.ID, hash); err != nil {
			return fmt.Errorf("reset failed: %w", err)
		}
		fmt.Printf("Password for %q has been reset.\n", cfg.AdminUser)
		return nil
	}

	if err := bootstrapAdmin(ctx, st, cfg, logger); err != nil {
		return fmt.Errorf("admin bootstrap failed: %w", err)
	}

	hub := api.NewHub()
	emailN := alerts.NewEmailNotifier(cfg)
	tgN := alerts.NewTelegramNotifier(cfg)
	engine := alerts.New(st, cfg, logger, hub, emailN, tgN)

	// Persist the default agent config on first start so settings page shows it.
	if _, ok := st.GetSetting(ctx, "default_agent_config"); !ok {
		def := models.DefaultAgentConfig()
		def.HeartbeatIntervalSec = cfg.HeartbeatIntervalSec
		def.MetricsIntervalSec = cfg.MetricsIntervalSec
		def.InventoryIntervalSec = cfg.InventoryIntervalSec
		b, _ := jsonMarshal(def)
		_ = st.SetSetting(ctx, "default_agent_config", string(b))
	}
	if _, ok := st.GetSetting(ctx, "server_name"); !ok {
		host, _ := os.Hostname()
		_ = st.SetSetting(ctx, "server_name", "PC Inventory Manager ("+host+")")
	}

	srv := &api.Server{
		Cfg: cfg, Store: st, Log: logger, Hub: hub, Alerts: engine, Email: emailN, Telegram: tgN,
		Disc: discovery.New(cfg.Port), StartedAt: time.Now(), BuildInfo: version.Version,
	}

	// Background jobs
	bgCtx, bgCancel := context.WithCancel(context.Background())
	defer bgCancel()
	go runBackground(bgCtx, srv, engine, logger)

	httpServer := &http.Server{
		Addr:              net.JoinHostPort(cfg.ListenHost, strconv.Itoa(cfg.Port)),
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       60 * time.Second,
		WriteTimeout:      0, // SSE needs no write timeout
		IdleTimeout:       120 * time.Second,
		MaxHeaderBytes:    1 << 20,
	}

	errCh := make(chan error, 1)
	scheme := cfg.Scheme()
	switch {
	case cfg.TLSEnabled():
		cert, err := tls.LoadX509KeyPair(cfg.TLSCertFile, cfg.TLSKeyFile)
		if err != nil {
			return fmt.Errorf("cannot load TLS certificate: %w", err)
		}
		httpServer.TLSConfig = &tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS12}
		go func() { errCh <- httpServer.ListenAndServeTLS("", "") }()
	case cfg.TLSAutoSelfSigned:
		cert, err := selfSignedCert(cfg)
		if err != nil {
			return fmt.Errorf("cannot create self-signed certificate: %w", err)
		}
		httpServer.TLSConfig = &tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS12}
		scheme = "https"
		go func() { errCh <- httpServer.ListenAndServeTLS("", "") }()
	default:
		go func() { errCh <- httpServer.ListenAndServe() }()
	}

	logger.Info("PC Inventory Manager server started",
		"version", version.Version, "listen", httpServer.Addr, "scheme", scheme,
		"dataDir", cfg.DataDir, "db", cfg.DBPath)
	logger.Info("dashboard", "url", fmt.Sprintf("%s://%s:%d", scheme, displayHost(cfg.ListenHost), cfg.Port))
	if !asService {
		fmt.Printf("\n  PC Inventory Manager %s\n  ---------------------------------\n  Dashboard:  %s://%s:%d\n  Agents:     %s\n  Data:       %s\n\n",
			version.Version, scheme, displayHost(cfg.ListenHost), cfg.Port, cfg.AdvertisedURL(), cfg.DataDir)
	}

	select {
	case <-ctx.Done():
		logger.Info("shutting down")
	case err := <-errCh:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			_ = st.AddServerEvent(context.Background(), "error", "http", err.Error())
			return fmt.Errorf("server error: %w", err)
		}
	}
	shutCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = httpServer.Shutdown(shutCtx)
	bgCancel()
	time.Sleep(200 * time.Millisecond)
	return nil
}

func displayHost(h string) string {
	if h == "" || h == "0.0.0.0" || h == "::" {
		if ip := primaryIP(); ip != "" {
			return ip
		}
		return "localhost"
	}
	return h
}

func primaryIP() string {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return ""
	}
	for _, a := range addrs {
		if ipnet, ok := a.(*net.IPNet); ok && !ipnet.IP.IsLoopback() && ipnet.IP.To4() != nil {
			return ipnet.IP.String()
		}
	}
	return ""
}

func bootstrapAdmin(ctx context.Context, st *store.Store, cfg config.Config, log *slog.Logger) error {
	n, err := st.CountAdmins(ctx)
	if err != nil {
		return err
	}
	if n > 0 {
		return nil
	}
	password := cfg.AdminPassword
	generated := false
	if password == "" {
		password, err = auth.NewToken()
		if err != nil {
			return err
		}
		password = password[:16]
		generated = true
	}
	hash, err := auth.HashPassword(password)
	if err != nil {
		return err
	}
	if _, err := st.CreateAdmin(ctx, cfg.AdminUser, hash); err != nil {
		return err
	}
	_ = st.AddAudit(ctx, "system", "admin.bootstrap", cfg.AdminUser, "", "")
	if generated {
		credFile := filepath.Join(cfg.DataDir, "initial-admin-password.txt")
		_ = os.WriteFile(credFile, []byte(fmt.Sprintf("username: %s\npassword: %s\n\nChange this password after first login and delete this file.\n", cfg.AdminUser, password)), 0o600)
		fmt.Printf("\n  First start: an administrator account was created.\n  Username: %s\n  Password: %s\n  (also written to %s)\n\n", cfg.AdminUser, password, credFile)
		log.Warn("bootstrap admin created with generated password", "file", credFile)
	} else {
		log.Info("bootstrap admin created", "user", cfg.AdminUser)
	}
	return nil
}

func jsonMarshal(v any) ([]byte, error) {
	return marshalJSON(v)
}

// selfSignedCert generates an in-memory self-signed certificate for LAN HTTPS.
func selfSignedCert(cfg config.Config) (tls.Certificate, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return tls.Certificate{}, err
	}
	serial, _ := rand.Int(rand.Reader, big.NewInt(1<<62))
	host, _ := os.Hostname()
	tmpl := x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: "PC Inventory Manager"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().AddDate(10, 0, 0),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames:     []string{"localhost", host},
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1"), net.ParseIP("::1")},
	}
	if ip := primaryIP(); ip != "" {
		if p := net.ParseIP(ip); p != nil {
			tmpl.IPAddresses = append(tmpl.IPAddresses, p)
		}
	}
	if cfg.ListenHost != "" && cfg.ListenHost != "0.0.0.0" {
		if p := net.ParseIP(cfg.ListenHost); p != nil {
			tmpl.IPAddresses = append(tmpl.IPAddresses, p)
		}
	}
	der, err := x509.CreateCertificate(rand.Reader, &tmpl, &tmpl, &key.PublicKey, key)
	if err != nil {
		return tls.Certificate{}, err
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return tls.Certificate{}, err
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})
	// Persist so the certificate stays stable across restarts.
	_ = os.MkdirAll(cfg.DataDir, 0o750)
	_ = os.WriteFile(filepath.Join(cfg.DataDir, "server-selfsigned.crt"), certPEM, 0o644)
	_ = os.WriteFile(filepath.Join(cfg.DataDir, "server-selfsigned.key"), keyPEM, 0o600)
	return tls.X509KeyPair(certPEM, keyPEM)
}

func newLogger(cfg config.Config) *slog.Logger {
	level := slog.LevelInfo
	switch strings.ToLower(cfg.LogLevel) {
	case "debug":
		level = slog.LevelDebug
	case "warn":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	}
	var w io.Writer = os.Stdout
	if cfg.LogFile != "" {
		_ = os.MkdirAll(filepath.Dir(cfg.LogFile), 0o750)
		if f, err := os.OpenFile(cfg.LogFile, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o640); err == nil {
			w = io.MultiWriter(os.Stdout, f)
		}
	}
	return slog.New(slog.NewTextHandler(w, &slog.HandlerOptions{Level: level}))
}
