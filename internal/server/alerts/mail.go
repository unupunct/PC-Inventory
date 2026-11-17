package alerts

import (
	"bytes"
	"encoding/base64"
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/smtp"
	"strings"
	"time"

	"github.com/pcim/pc-inventory-manager/internal/server/config"
)

// Attachment is a file to send with a message.
type Attachment struct {
	Filename    string
	ContentType string
	Data        []byte
}

// SendMailWithAttachments sends a plain-text message with file attachments
// through the configured SMTP server. Recipients are comma separated.
func (n *EmailNotifier) SendMailWithAttachments(ctx context.Context, to []string, subject, body string, atts []Attachment) error {
	if n.cfg.SMTPHost == "" {
		return fmt.Errorf("SMTP is not configured")
	}
	var rcpts []string
	for _, r := range to {
		r = strings.TrimSpace(r)
		if r != "" {
			rcpts = append(rcpts, r)
		}
	}
	if len(rcpts) == 0 {
		return fmt.Errorf("no recipients")
	}
	from := n.cfg.SMTPFrom
	if from == "" {
		from = n.cfg.SMTPUser
	}
	if from == "" {
		return fmt.Errorf("SMTP sender is not configured")
	}

	boundary := fmt.Sprintf("pcim-%d", time.Now().UnixNano())
	var b bytes.Buffer
	b.WriteString("From: " + from + "\r\n")
	b.WriteString("To: " + strings.Join(rcpts, ", ") + "\r\n")
	b.WriteString("Subject: " + subject + "\r\n")
	b.WriteString("Date: " + time.Now().Format(time.RFC1123Z) + "\r\n")
	b.WriteString("MIME-Version: 1.0\r\n")
	b.WriteString("Content-Type: multipart/mixed; boundary=\"" + boundary + "\"\r\n\r\n")

	b.WriteString("--" + boundary + "\r\n")
	b.WriteString("Content-Type: text/plain; charset=utf-8\r\n\r\n")
	b.WriteString(body + "\r\n\r\n")

	for _, a := range atts {
		ct := a.ContentType
		if ct == "" {
			ct = "application/octet-stream"
		}
		b.WriteString("--" + boundary + "\r\n")
		b.WriteString("Content-Type: " + ct + "; name=\"" + a.Filename + "\"\r\n")
		b.WriteString("Content-Disposition: attachment; filename=\"" + a.Filename + "\"\r\n")
		b.WriteString("Content-Transfer-Encoding: base64\r\n\r\n")
		b.WriteString(base64Wrap(a.Data))
		b.WriteString("\r\n")
	}
	b.WriteString("--" + boundary + "--\r\n")

	var auth smtp.Auth
	if n.cfg.SMTPUser != "" {
		auth = smtp.PlainAuth("", n.cfg.SMTPUser, n.cfg.SMTPPassword, n.cfg.SMTPHost)
	}
	addr := net.JoinHostPort(n.cfg.SMTPHost, fmt.Sprint(n.cfg.SMTPPort))

	if n.cfg.SMTPPort == 465 {
		d := &net.Dialer{Timeout: 20 * time.Second}
		conn, err := tls.DialWithDialer(d, "tcp", addr, &tls.Config{ServerName: n.cfg.SMTPHost})
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
		for _, r := range rcpts {
			if err := c.Rcpt(r); err != nil {
				return err
			}
		}
		w, err := c.Data()
		if err != nil {
			return err
		}
		if _, err := w.Write(b.Bytes()); err != nil {
			return err
		}
		if err := w.Close(); err != nil {
			return err
		}
		return c.Quit()
	}
	return smtp.SendMail(addr, auth, from, rcpts, b.Bytes())
}

func base64Wrap(data []byte) string {
	const lineLen = 76
	enc := base64.StdEncoding.EncodeToString(data)
	var out strings.Builder
	for len(enc) > lineLen {
		out.WriteString(enc[:lineLen])
		out.WriteString("\r\n")
		enc = enc[lineLen:]
	}
	out.WriteString(enc)
	return out.String()
}

// NewEmailNotifierFor is a convenience constructor for the scheduler.
func NewEmailNotifierFor(cfg config.Config) *EmailNotifier { return NewEmailNotifier(cfg) }
