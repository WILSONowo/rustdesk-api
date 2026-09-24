package service

import (
	"context"
	"crypto/tls"
	"encoding/base64"
	"errors"
	"fmt"
	"mime"
	"net"
	"net/mail"
	"net/smtp"
	"strconv"
	"strings"
	"time"

	"github.com/lejianwen/rustdesk-api/v2/model"
	"gorm.io/gorm"
)

// One worker per process; an atomic lease also prevents concurrent processes
// from claiming the same row. SMTP can deliver twice after an ambiguous timeout.
func (s *MailService) DeliverOne(ctx context.Context) error {
	if !s.Ready() {
		return ErrMailUnavailable
	}
	now := time.Now()
	var msg model.MailMessage
	err := s.db.Where("state = ? AND available_at <= ? OR state = ? AND available_at <= ?", "queued", now, "sending", now).Order("id").First(&msg).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if !now.Before(msg.ExpiresAt) {
		return s.db.Model(&msg).Updates(map[string]interface{}{"state": "expired", "body": ""}).Error
	}
	if msg.ChallengeID != "" {
		var ch model.EmailChallenge
		if s.db.First(&ch, "id = ?", msg.ChallengeID).Error != nil || ch.Consumed || !now.Before(ch.ExpiresAt) {
			return s.db.Model(&msg).Updates(map[string]interface{}{"state": "expired", "body": ""}).Error
		}
	}
	claimed := false
	err = s.db.Transaction(func(tx *gorm.DB) error {
		if err := mailQuota(tx, "delivery-day", s.cfg.DailyLimit, 24*time.Hour); err != nil {
			return err
		}
		r := tx.Model(&model.MailMessage{}).Where("id = ? AND state = ? AND available_at <= ?", msg.ID, msg.State, now).Updates(map[string]interface{}{"state": "sending", "available_at": now.Add(2 * time.Minute), "attempts": gorm.Expr("attempts + 1")})
		claimed = r.RowsAffected == 1
		return r.Error
	})
	if err != nil || !claimed {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	err = s.Send(ctx, msg)
	values := map[string]interface{}{"state": "sent", "body": "", "error_code": ""}
	if err != nil {
		attempts := msg.Attempts + 1
		values = map[string]interface{}{"state": "queued", "error_code": "smtp_delivery_failed", "available_at": time.Now().Add(time.Duration(1<<uint(attempts)) * 15 * time.Second)}
		if attempts >= 5 {
			values["state"] = "failed"
			values["body"] = ""
		}
	}
	return s.db.Model(&msg).Updates(values).Error
}

func (s *MailService) Run(ctx context.Context) {
	if !s.Ready() {
		return
	}
	ticker := time.NewTicker(time.Duration(s.cfg.SendIntervalSeconds) * time.Second)
	defer ticker.Stop()
	cleanup := time.NewTicker(time.Hour)
	defer cleanup.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := s.DeliverOne(ctx); err != nil && !errors.Is(err, ErrMailRate) {
				Logger.Warn("Mail queue processing failed; check database and SMTP configuration")
			}
		case <-cleanup.C:
			now := time.Now()
			s.db.Where("expires_at < ?", now.Add(-24*time.Hour)).Delete(&model.EmailChallenge{})
			s.db.Where("expires_at < ?", now).Delete(&model.MailRate{})
			s.db.Model(&model.MailMessage{}).Where("expires_at < ? AND state IN ?", now, []string{"queued", "sending"}).Updates(map[string]interface{}{"state": "expired", "body": ""})
			s.db.Where("created_at < ? AND state IN ?", now.Add(-7*24*time.Hour), []string{"sent", "failed", "expired"}).Delete(&model.MailMessage{})
		}
	}
}

type loginAuth struct {
	username, password string
	step               int
}

func (a *loginAuth) Start(server *smtp.ServerInfo) (string, []byte, error) {
	if !server.TLS {
		return "", nil, errors.New("TLS required for authentication")
	}
	a.step = 0
	return "LOGIN", nil, nil
}
func (a *loginAuth) Next(_ []byte, more bool) ([]byte, error) {
	if !more {
		return nil, nil
	}
	a.step++
	if a.step == 1 {
		return []byte(a.username), nil
	}
	if a.step == 2 {
		return []byte(a.password), nil
	}
	return nil, errors.New("unexpected SMTP authentication challenge")
}

func (s *MailService) sendSMTP(ctx context.Context, msg model.MailMessage) error {
	to, err := NormalizeEmail(msg.To)
	if err != nil {
		return err
	}
	from, err := NormalizeEmail(s.cfg.From)
	if err != nil {
		return err
	}
	addr := net.JoinHostPort(s.cfg.Host, strconv.Itoa(s.cfg.Port))
	dialer := &net.Dialer{Timeout: 10 * time.Second}
	conn, err := dialer.DialContext(ctx, "tcp", addr)
	if err != nil {
		return err
	}
	defer conn.Close()
	deadline := time.Now().Add(30 * time.Second)
	if d, ok := ctx.Deadline(); ok {
		deadline = d
	}
	_ = conn.SetDeadline(deadline)
	tlsConfig := &tls.Config{ServerName: s.cfg.Host, MinVersion: tls.VersionTLS12}
	if s.cfg.TLS == "none" {
		if tcp, ok := conn.RemoteAddr().(*net.TCPAddr); !ok || !tcp.IP.IsLoopback() {
			return errors.New("plaintext SMTP is restricted to loopback testing")
		}
	}
	if s.cfg.TLS == "implicit" {
		secure := tls.Client(conn, tlsConfig)
		if err := secure.HandshakeContext(ctx); err != nil {
			return err
		}
		conn = secure
	}
	client, err := smtp.NewClient(conn, s.cfg.Host)
	if err != nil {
		return err
	}
	defer client.Close()
	if s.cfg.TLS == "starttls" {
		if err := client.StartTLS(tlsConfig); err != nil {
			return err
		}
	}
	if s.cfg.Username != "" {
		_, methods := client.Extension("AUTH")
		var auth smtp.Auth = smtp.PlainAuth("", s.cfg.Username, s.cfg.Password, s.cfg.Host)
		if !strings.Contains(strings.ToUpper(methods), "PLAIN") && strings.Contains(strings.ToUpper(methods), "LOGIN") {
			auth = &loginAuth{username: s.cfg.Username, password: s.cfg.Password}
		}
		if err := client.Auth(auth); err != nil {
			return err
		}
	}
	if err := client.Mail(from); err != nil {
		return err
	}
	if err := client.Rcpt(to); err != nil {
		return err
	}
	w, err := client.Data()
	if err != nil {
		return err
	}
	name := mail.Address{Name: s.cfg.FromName, Address: from}
	body := base64.StdEncoding.EncodeToString([]byte(msg.Body))
	var wrapped strings.Builder
	for len(body) > 76 {
		wrapped.WriteString(body[:76] + "\r\n")
		body = body[76:]
	}
	wrapped.WriteString(body + "\r\n")
	message := fmt.Sprintf("From: %s\r\nTo: %s\r\nSubject: %s\r\nDate: %s\r\nMessage-ID: <%d.%d@%s>\r\nMIME-Version: 1.0\r\nContent-Type: text/plain; charset=UTF-8\r\nContent-Transfer-Encoding: base64\r\n\r\n%s", name.String(), to, mime.QEncoding.Encode("UTF-8", msg.Subject), time.Now().Format(time.RFC1123Z), msg.ID, msg.CreatedAt.UnixNano(), strings.SplitN(from, "@", 2)[1], wrapped.String())
	if _, err = w.Write([]byte(message)); err != nil {
		return err
	}
	if err = w.Close(); err != nil {
		return err
	}
	_ = client.Quit()
	return nil
}
