package alerts

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/smtp"
	"strings"
	"time"

	"github.com/pcim/pc-inventory-manager/internal/server/config"
	"github.com/pcim/pc-inventory-manager/internal/server/store"
)

// EmailNotifier sends alerts via SMTP.
type EmailNotifier struct {
	cfg config.Config
}

func NewEmailNotifier(cfg config.Config) *EmailNotifier { return &EmailNotifier{cfg: cfg} }

func (n *EmailNotifier) Name() string { return "email" }

func (n *EmailNotifier) Configured() bool {
	return n.cfg.SMTPHost != "" && n.cfg.AlertEmailTo != ""
}

func (n *EmailNotifier) Send(ctx context.Context, a store.Alert, rule store.AlertRule) error {
	if !n.Configured() {
		return fmt.Errorf("email notifications not configured")
	}
	from := n.cfg.SMTPFrom
	if from == "" {
		from = n.cfg.SMTPUser
	}
	var auth smtp.Auth
	if n.cfg.SMTPUser != "" {
		auth = smtp.PlainAuth("", n.cfg.SMTPUser, n.cfg.SMTPPassword, n.cfg.SMTPHost)
	}
	subject := fmt.Sprintf("[PC Inventory Manager] [%s] %s", strings.ToUpper(a.Severity), a.Title)
	body := fmt.Sprintf("%s\r\n\r\nSeverity: %s\r\nStatus: %s\r\nOpened: %s\r\n\r\n%s\r\n", a.Title, a.Severity, a.Status, a.OpenedAt.Local().Format(time.RFC1123), a.Message)
	msg := []byte("From: " + from + "\r\nTo: " + n.cfg.AlertEmailTo + "\r\nSubject: " + subject + "\r\nDate: " + time.Now().Format(time.RFC1123Z) + "\r\nMIME-Version: 1.0\r\nContent-Type: text/plain; charset=utf-8\r\n\r\n" + body)
	addr := net.JoinHostPort(n.cfg.SMTPHost, fmt.Sprint(n.cfg.SMTPPort))
	if n.cfg.SMTPPort == 465 {
		conn, err := tls.Dial("tcp", addr, &tls.Config{ServerName: n.cfg.SMTPHost})
		if err != nil {
			return err
		}
		defer conn.Close()
		c, err := smtp.NewClient(conn, n.cfg.SMTPHost)
		if err != nil {
			return err
		}
		defer c.Close()
		if auth != nil {
			if err := c.Auth(auth); err != nil {
				return err
			}
		}
		if err := c.Mail(from); err != nil {
			return err
		}
		if err := c.Rcpt(n.cfg.AlertEmailTo); err != nil {
			return err
		}
		w, err := c.Data()
		if err != nil {
			return err
		}
		if _, err := w.Write(msg); err != nil {
			return err
		}
		if err := w.Close(); err != nil {
			return err
		}
		return c.Quit()
	}
	return smtp.SendMail(addr, auth, from, []string{n.cfg.AlertEmailTo}, msg)
}

// TelegramNotifier sends alerts through the Telegram Bot API.
type TelegramNotifier struct {
	cfg config.Config
	hc  *http.Client
}

func NewTelegramNotifier(cfg config.Config) *TelegramNotifier {
	return &TelegramNotifier{cfg: cfg, hc: &http.Client{Timeout: 15 * time.Second}}
}

func (n *TelegramNotifier) Name() string { return "telegram" }

func (n *TelegramNotifier) Configured() bool {
	return n.cfg.TelegramToken != "" && n.cfg.TelegramChatID != ""
}

func (n *TelegramNotifier) Send(ctx context.Context, a store.Alert, rule store.AlertRule) error {
	if !n.Configured() {
		return fmt.Errorf("telegram notifications not configured")
	}
	text := fmt.Sprintf("⚠️ *%s*\nSeverity: %s\nStatus: %s\n%s", escapeMD(a.Title), a.Severity, a.Status, escapeMD(a.Message))
	payload := map[string]any{"chat_id": n.cfg.TelegramChatID, "text": text, "parse_mode": "MarkdownV2"}
	b, _ := json.Marshal(payload)
	url := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", n.cfg.TelegramToken)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(b))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := n.hc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		var buf bytes.Buffer
		buf.ReadFrom(resp.Body)
		return fmt.Errorf("telegram API %d: %s", resp.StatusCode, buf.String())
	}
	return nil
}

func escapeMD(s string) string {
	r := strings.NewReplacer("_", "\\_", "*", "\\*", "[", "\\[", "]", "\\]", "(", "\\(", ")", "\\)", "~", "\\~", "`", "\\`", ">", "\\>", "#", "\\#", "+", "\\+", "-", "\\-", "=", "\\=", "|", "\\|", "{", "\\{", "}", "\\}", ".", "\\.", "!", "\\!")
	return r.Replace(s)
}

// TestEmail sends a test message.
func (n *EmailNotifier) Test(ctx context.Context) error {
	return n.Send(ctx, store.Alert{Title: "Test notification", Severity: "info", Status: "open", Message: "This is a test notification from PC Inventory Manager.", OpenedAt: time.Now()}, store.AlertRule{})
}

// TestTelegram sends a test message.
func (n *TelegramNotifier) Test(ctx context.Context) error {
	return n.Send(ctx, store.Alert{Title: "Test notification", Severity: "info", Status: "open", Message: "This is a test notification from PC Inventory Manager.", OpenedAt: time.Now()}, store.AlertRule{})
}
