package mailer

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/tls"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"mime"
	"net"
	"net/smtp"
	"strings"
	"time"

	"github.com/jeffhou369/wurzburg/backend/internal/clock"
	"github.com/jeffhou369/wurzburg/backend/internal/interfaces"
	"github.com/jeffhou369/wurzburg/backend/internal/settings"
)

// SMTPMailer 實作 interfaces.Mailer，組裝符合 multipart/alternative 標準的郵件、
// 支援 STARTTLS / SMTPS / 無加密，並具備指數退避有限次重試機制。
type SMTPMailer struct {
	db       *sql.DB
	clock    clock.Clock
	settings *settings.Store
	dialFn   func(ctx context.Context, cfg SMTPConfig, msg interfaces.MailMessage, raw []byte) error
}

type SMTPConfig struct {
	Host     string
	Port     int
	TLSMode  string // starttls | smtps | none
	Username string
	Password string
	Sender   string
}

func NewSMTPMailer(db *sql.DB, clk clock.Clock, sett *settings.Store) *SMTPMailer {
	if clk == nil {
		clk = clock.NewRealClock()
	}
	m := &SMTPMailer{
		db:       db,
		clock:    clk,
		settings: sett,
	}
	m.dialFn = m.sendViaNetwork
	return m
}

func (m *SMTPMailer) LoadConfig(ctx context.Context) (SMTPConfig, error) {
	if m.settings == nil {
		return SMTPConfig{}, errors.New("設定儲存庫未初始化")
	}
	host, _ := m.settings.Get(ctx, "smtp_host")
	port := m.settings.GetInt(ctx, "smtp_port", 587)
	tlsMode, _ := m.settings.Get(ctx, "smtp_tls_mode")
	if tlsMode == "" {
		tlsMode = "starttls"
	}
	user, _ := m.settings.Get(ctx, "smtp_username")
	pass, _ := m.settings.GetSecret(ctx, "smtp_password")
	sender, _ := m.settings.Get(ctx, "smtp_sender")

	if host == "" || sender == "" {
		return SMTPConfig{}, errors.New("SMTP 主機或寄件人信箱尚未設定")
	}

	return SMTPConfig{
		Host:     host,
		Port:     port,
		TLSMode:  tlsMode,
		Username: user,
		Password: pass,
		Sender:   sender,
	}, nil
}

// Send 寄送郵件（失敗最多重試 3 次，間隔 1s -> 2s -> 4s），並自動記錄 notification_logs。
func (m *SMTPMailer) Send(ctx context.Context, msg interfaces.MailMessage) error {
	_, err := m.SendWithTracking(ctx, "test_mail", "", msg)
	return err
}

// SendWithTracking 寄送並寫入通知紀錄，回傳嘗試次數與錯誤。
func (m *SMTPMailer) SendWithTracking(ctx context.Context, notifType, dedupKey string, msg interfaces.MailMessage) (int, error) {
	cfg, err := m.LoadConfig(ctx)
	if err != nil {
		m.writeLog(ctx, notifType, dedupKey, msg, "failed", 1, err.Error())
		return 1, err
	}

	raw, err := BuildMultipartAlternativeMIME(cfg.Sender, msg)
	if err != nil {
		m.writeLog(ctx, notifType, dedupKey, msg, "failed", 1, err.Error())
		return 1, err
	}

	maxAttempts := 3
	var lastErr error
	attempts := 0

	for attempt := 1; attempt <= maxAttempts; attempt++ {
		attempts = attempt
		lastErr = m.dialFn(ctx, cfg, msg, raw)
		if lastErr == nil {
			m.writeLog(ctx, notifType, dedupKey, msg, "sent", attempts, "")
			return attempts, nil
		}
		if attempt < maxAttempts {
			backoff := time.Duration(1<<(attempt-1)) * time.Second
			m.clock.Sleep(backoff)
		}
	}

	m.writeLog(ctx, notifType, dedupKey, msg, "failed", attempts, lastErr.Error())
	return attempts, lastErr
}

func (m *SMTPMailer) writeLog(ctx context.Context, notifType, dedupKey string, msg interfaces.MailMessage, status string, attempts int, errStr string) {
	if m.db == nil {
		return
	}
	now := m.clock.Now()
	var sentAt *time.Time
	if status == "sent" {
		sentAt = &now
	}
	recipients := strings.Join(msg.To, ", ")
	if len(msg.Cc) > 0 {
		recipients += " (Cc: " + strings.Join(msg.Cc, ", ") + ")"
	}
	_, _ = m.db.ExecContext(
		ctx,
		`INSERT INTO notification_logs (notif_type, dedup_key, recipient, subject, status, attempt_count, last_error, sent_at, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		notifType, dedupKey, recipients, msg.Subject, status, attempts, errStr, sentAt, now,
	)
}

// BuildMultipartAlternativeMIME 產生標準 RFC 2046 multipart/alternative 郵件本文。
func BuildMultipartAlternativeMIME(from string, msg interfaces.MailMessage) ([]byte, error) {
	bndBytes := make([]byte, 12)
	_, _ = rand.Read(bndBytes)
	boundary := "wurzburg_" + hex.EncodeToString(bndBytes)

	var buf bytes.Buffer
	encodedSub := mime.QEncoding.Encode("UTF-8", msg.Subject)
	buf.WriteString(fmt.Sprintf("From: %s\r\n", from))
	buf.WriteString(fmt.Sprintf("To: %s\r\n", strings.Join(msg.To, ", ")))
	if len(msg.Cc) > 0 {
		buf.WriteString(fmt.Sprintf("Cc: %s\r\n", strings.Join(msg.Cc, ", ")))
	}
	buf.WriteString(fmt.Sprintf("Subject: %s\r\n", encodedSub))
	buf.WriteString("MIME-Version: 1.0\r\n")
	buf.WriteString(fmt.Sprintf("Content-Type: multipart/alternative; boundary=\"%s\"\r\n", boundary))
	buf.WriteString("\r\n")

	// 1. 純文字部分 (text/plain)
	buf.WriteString(fmt.Sprintf("--%s\r\n", boundary))
	buf.WriteString("Content-Type: text/plain; charset=UTF-8\r\n")
	buf.WriteString("Content-Transfer-Encoding: base64\r\n\r\n")
	writeBase64Wrapped(&buf, []byte(msg.TextBody))
	buf.WriteString("\r\n")

	// 2. HTML 部分 (text/html)
	buf.WriteString(fmt.Sprintf("--%s\r\n", boundary))
	buf.WriteString("Content-Type: text/html; charset=UTF-8\r\n")
	buf.WriteString("Content-Transfer-Encoding: base64\r\n\r\n")
	writeBase64Wrapped(&buf, []byte(msg.HTMLBody))
	buf.WriteString("\r\n")

	buf.WriteString(fmt.Sprintf("--%s--\r\n", boundary))
	return buf.Bytes(), nil
}

func writeBase64Wrapped(buf *bytes.Buffer, data []byte) {
	encoded := base64.StdEncoding.EncodeToString(data)
	for len(encoded) > 76 {
		buf.WriteString(encoded[:76] + "\r\n")
		encoded = encoded[76:]
	}
	if len(encoded) > 0 {
		buf.WriteString(encoded + "\r\n")
	}
}

func (m *SMTPMailer) sendViaNetwork(ctx context.Context, cfg SMTPConfig, msg interfaces.MailMessage, raw []byte) error {
	addr := fmt.Sprintf("%s:%d", cfg.Host, cfg.Port)
	var client *smtp.Client
	var err error

	dialer := &net.Dialer{Timeout: 10 * time.Second}
	if cfg.TLSMode == "smtps" {
		conn, dialErr := tls.DialWithDialer(dialer, "tcp", addr, &tls.Config{ServerName: cfg.Host})
		if dialErr != nil {
			return fmt.Errorf("SMTPS 連線失敗: %w", dialErr)
		}
		client, err = smtp.NewClient(conn, cfg.Host)
	} else {
		conn, dialErr := dialer.DialContext(ctx, "tcp", addr)
		if dialErr != nil {
			return fmt.Errorf("SMTP 連線失敗: %w", dialErr)
		}
		client, err = smtp.NewClient(conn, cfg.Host)
	}
	if err != nil {
		return err
	}
	defer func() { _ = client.Close() }()

	if cfg.TLSMode == "starttls" {
		if ok, _ := client.Extension("STARTTLS"); ok {
			if err := client.StartTLS(&tls.Config{ServerName: cfg.Host}); err != nil {
				return fmt.Errorf("STARTTLS 交握失敗: %w", err)
			}
		}
	}

	if cfg.Username != "" {
		a := smtp.PlainAuth("", cfg.Username, cfg.Password, cfg.Host)
		if err := client.Auth(a); err != nil {
			return fmt.Errorf("SMTP 認證失敗: %w", err)
		}
	}

	if err := client.Mail(cfg.Sender); err != nil {
		return err
	}

	allRecipients := append([]string{}, msg.To...)
	allRecipients = append(allRecipients, msg.Cc...)
	for _, rcpt := range allRecipients {
		if err := client.Rcpt(strings.TrimSpace(rcpt)); err != nil {
			return err
		}
	}

	w, err := client.Data()
	if err != nil {
		return err
	}
	if _, err := w.Write(raw); err != nil {
		return err
	}
	if err := w.Close(); err != nil {
		return err
	}
	return client.Quit()
}
