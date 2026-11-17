// Package config loads server configuration from an env file and environment
// variables.
package config

import (
	"bufio"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

type Config struct {
	// Network
	ListenHost string // e.g. 127.0.0.1 or 0.0.0.0
	Port       int
	PublicURL  string // advertised base URL for agents, e.g. http://192.168.1.100:8080

	// TLS
	TLSCertFile string
	TLSKeyFile  string
	// If TLS is not configured but enabled is true, a self-signed cert is generated.
	TLSAutoSelfSigned bool

	// Storage
	DataDir        string
	DBPath         string
	BackupDir      string
	RetentionDays  int
	RawSampleHours int

	// Admin bootstrap
	AdminUser     string
	AdminPassword string

	// Behaviour
	OfflineThresholdSec  int
	HeartbeatIntervalSec int
	MetricsIntervalSec   int
	InventoryIntervalSec int
	SessionTTLHours      int
	AllowedCIDRs         []string

	// Notifications
	SMTPHost       string
	SMTPPort       int
	SMTPUser       string
	SMTPPassword   string
	SMTPFrom       string
	SMTPTLS        bool
	AlertEmailTo   string
	TelegramToken  string
	TelegramChatID string

	// Logging
	LogLevel string
	LogFile  string

	// Security
	TrustProxy   bool
	MaxBodyBytes int64
}

func Default() Config {
	wd, _ := os.Getwd()
	dataDir := filepath.Join(wd, "data")
	return Config{
		ListenHost:           "127.0.0.1",
		Port:                 8080,
		DataDir:              dataDir,
		DBPath:               filepath.Join(dataDir, "pcim.db"),
		BackupDir:            filepath.Join(dataDir, "backups"),
		RetentionDays:        30,
		RawSampleHours:       48,
		AdminUser:            "admin",
		OfflineThresholdSec:  90,
		HeartbeatIntervalSec: 30,
		MetricsIntervalSec:   30,
		InventoryIntervalSec: 6 * 3600,
		SessionTTLHours:      72,
		SMTPPort:             587,
		SMTPTLS:              true,
		LogLevel:             "info",
		MaxBodyBytes:         8 << 20,
	}
}

// Load reads a key=value env file (if present) and then applies OS env vars.
func Load(envFile string) (Config, error) {
	c := Default()
	if envFile == "" {
		envFile = ".env"
	}
	if _, err := os.Stat(envFile); err == nil {
		if err := c.loadFile(envFile); err != nil {
			return c, err
		}
	}
	c.applyEnv()
	c.finalize()
	if err := c.Validate(); err != nil {
		return c, err
	}
	return c, nil
}

func (c *Config) loadFile(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		k = strings.TrimSpace(k)
		v = strings.TrimSpace(v)
		v = strings.Trim(v, `"'`)
		c.set(k, v)
	}
	return sc.Err()
}

func (c *Config) set(k, v string) {
	switch strings.ToUpper(k) {
	case "PCIM_LISTEN_HOST", "LISTEN_HOST":
		c.ListenHost = v
	case "PCIM_PORT", "PORT":
		if n, err := strconv.Atoi(v); err == nil {
			c.Port = n
		}
	case "PCIM_PUBLIC_URL", "PUBLIC_URL":
		c.PublicURL = v
	case "PCIM_TLS_CERT", "TLS_CERT":
		c.TLSCertFile = v
	case "PCIM_TLS_KEY", "TLS_KEY":
		c.TLSKeyFile = v
	case "PCIM_TLS_SELF_SIGNED", "TLS_SELF_SIGNED":
		c.TLSAutoSelfSigned = parseBool(v)
	case "PCIM_DATA_DIR", "DATA_DIR":
		c.DataDir = v
		// Keep the database and backups with the data directory unless they
		// were set explicitly later in the stream.
		c.DBPath = filepath.Join(v, "pcim.db")
		c.BackupDir = filepath.Join(v, "backups")
	case "PCIM_DB_PATH", "DB_PATH":
		c.DBPath = v
	case "PCIM_BACKUP_DIR", "BACKUP_DIR":
		c.BackupDir = v
	case "PCIM_RETENTION_DAYS", "RETENTION_DAYS":
		if n, err := strconv.Atoi(v); err == nil {
			c.RetentionDays = n
		}
	case "PCIM_RAW_SAMPLE_HOURS", "RAW_SAMPLE_HOURS":
		if n, err := strconv.Atoi(v); err == nil {
			c.RawSampleHours = n
		}
	case "PCIM_ADMIN_USER", "ADMIN_USER":
		c.AdminUser = v
	case "PCIM_ADMIN_PASSWORD", "ADMIN_PASSWORD":
		c.AdminPassword = v
	case "PCIM_OFFLINE_THRESHOLD_SEC", "OFFLINE_THRESHOLD_SEC":
		if n, err := strconv.Atoi(v); err == nil {
			c.OfflineThresholdSec = n
		}
	case "PCIM_HEARTBEAT_INTERVAL_SEC", "HEARTBEAT_INTERVAL_SEC":
		if n, err := strconv.Atoi(v); err == nil {
			c.HeartbeatIntervalSec = n
		}
	case "PCIM_METRICS_INTERVAL_SEC", "METRICS_INTERVAL_SEC":
		if n, err := strconv.Atoi(v); err == nil {
			c.MetricsIntervalSec = n
		}
	case "PCIM_INVENTORY_INTERVAL_SEC", "INVENTORY_INTERVAL_SEC":
		if n, err := strconv.Atoi(v); err == nil {
			c.InventoryIntervalSec = n
		}
	case "PCIM_SESSION_TTL_HOURS", "SESSION_TTL_HOURS":
		if n, err := strconv.Atoi(v); err == nil {
			c.SessionTTLHours = n
		}
	case "PCIM_ALLOWED_CIDRS", "ALLOWED_CIDRS":
		for _, p := range strings.Split(v, ",") {
			if p = strings.TrimSpace(p); p != "" {
				c.AllowedCIDRs = append(c.AllowedCIDRs, p)
			}
		}
	case "PCIM_SMTP_HOST", "SMTP_HOST":
		c.SMTPHost = v
	case "PCIM_SMTP_PORT", "SMTP_PORT":
		if n, err := strconv.Atoi(v); err == nil {
			c.SMTPPort = n
		}
	case "PCIM_SMTP_USER", "SMTP_USER":
		c.SMTPUser = v
	case "PCIM_SMTP_PASSWORD", "SMTP_PASSWORD":
		c.SMTPPassword = v
	case "PCIM_SMTP_FROM", "SMTP_FROM":
		c.SMTPFrom = v
	case "PCIM_SMTP_TLS", "SMTP_TLS":
		c.SMTPTLS = parseBool(v)
	case "PCIM_ALERT_EMAIL_TO", "ALERT_EMAIL_TO":
		c.AlertEmailTo = v
	case "PCIM_TELEGRAM_TOKEN", "TELEGRAM_TOKEN":
		c.TelegramToken = v
	case "PCIM_TELEGRAM_CHAT_ID", "TELEGRAM_CHAT_ID":
		c.TelegramChatID = v
	case "PCIM_LOG_LEVEL", "LOG_LEVEL":
		c.LogLevel = v
	case "PCIM_LOG_FILE", "LOG_FILE":
		c.LogFile = v
	case "PCIM_TRUST_PROXY", "TRUST_PROXY":
		c.TrustProxy = parseBool(v)
	}
}

func (c *Config) applyEnv() {
	for _, kv := range os.Environ() {
		k, v, ok := strings.Cut(kv, "=")
		if !ok {
			continue
		}
		if strings.HasPrefix(strings.ToUpper(k), "PCIM_") {
			c.set(k, v)
		}
	}
}

func (c *Config) finalize() {
	abs := func(p string) string {
		if p == "" {
			return p
		}
		if a, err := filepath.Abs(p); err == nil {
			return a
		}
		return p
	}
	c.DataDir = abs(c.DataDir)
	c.DBPath = abs(c.DBPath)
	c.BackupDir = abs(c.BackupDir)
	if c.TLSCertFile != "" {
		c.TLSCertFile = abs(c.TLSCertFile)
	}
	if c.TLSKeyFile != "" {
		c.TLSKeyFile = abs(c.TLSKeyFile)
	}
}

func (c Config) TLSEnabled() bool { return c.TLSCertFile != "" && c.TLSKeyFile != "" }

func (c Config) Scheme() string {
	if c.TLSEnabled() {
		return "https"
	}
	return "http"
}

// AdvertisedURL is the base URL agents should use.
func (c Config) AdvertisedURL() string {
	if c.PublicURL != "" {
		return strings.TrimRight(c.PublicURL, "/")
	}
	host := c.ListenHost
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = "localhost"
	}
	return fmt.Sprintf("%s://%s:%d", c.Scheme(), host, c.Port)
}

func (c Config) Validate() error {
	if c.Port < 1 || c.Port > 65535 {
		return fmt.Errorf("invalid port %d", c.Port)
	}
	if c.OfflineThresholdSec < 10 {
		c.OfflineThresholdSec = 10
	}
	for _, p := range c.AllowedCIDRs {
		if _, _, err := net.ParseCIDR(p); err != nil {
			if net.ParseIP(p) == nil {
				return fmt.Errorf("invalid CIDR/IP in PCIM_ALLOWED_CIDRS: %q", p)
			}
		}
	}
	if (c.TLSCertFile == "") != (c.TLSKeyFile == "") {
		return fmt.Errorf("both PCIM_TLS_CERT and PCIM_TLS_KEY must be set to enable HTTPS")
	}
	return nil
}

func parseBool(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "1", "true", "yes", "on", "enabled":
		return true
	}
	return false
}
