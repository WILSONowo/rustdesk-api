package service

import (
	"context"
	"errors"
	"io"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lejianwen/rustdesk-api/v2/config"
	"github.com/lejianwen/rustdesk-api/v2/lib/jwt"
	"github.com/lejianwen/rustdesk-api/v2/lib/lock"
	"github.com/lejianwen/rustdesk-api/v2/model"
	"github.com/lejianwen/rustdesk-api/v2/utils"
	"github.com/sirupsen/logrus"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func mailFixture(t *testing.T) (*MailService, *gorm.DB) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "mail.db")), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, _ := db.DB()
	t.Cleanup(func() { sqlDB.Close() })
	if err := db.AutoMigrate(&model.User{}, &model.UserToken{}, &model.EmailIdentity{}, &model.EmailChallenge{}, &model.MailMessage{}, &model.MailRate{}); err != nil {
		t.Fatal(err)
	}
	cfg := config.Config{Mail: config.Mail{Enabled: true, Host: "smtp.qq.com", Port: 465, From: "sender@example.test", TLS: "implicit", DailyLimit: 1000, PublicURL: "https://remote.neko-arc.top"}}
	l := logrus.New()
	l.SetOutput(io.Discard)
	New(&cfg, db, l, jwt.NewJwt("", time.Hour), lock.NewLocal())
	return AllService.MailService, db
}

func queuedCode(t *testing.T, db *gorm.DB, id string) string {
	t.Helper()
	var msg model.MailMessage
	if err := db.First(&msg, "challenge_id = ?", id).Error; err != nil {
		t.Fatal(err)
	}
	m := regexp.MustCompile(`验证码：(\d{8})`).FindStringSubmatch(msg.Body)
	if len(m) != 2 {
		t.Fatal("no code in queued message")
	}
	return m[1]
}

func clearMailRates(t *testing.T, db *gorm.DB) {
	t.Helper()
	if err := db.Where("1 = 1").Delete(&model.MailRate{}).Error; err != nil {
		t.Fatal(err)
	}
}

func TestVerifiedRegistrationAndRecovery(t *testing.T) {
	s, db := mailFixture(t)
	id, err := s.RequestCode("register", " Alice@Example.test ", 0, "", "ip1")
	if err != nil {
		t.Fatal(err)
	}
	code := queuedCode(t, db, id)
	var challenge model.EmailChallenge
	db.First(&challenge, "id = ?", id)
	if strings.Contains(challenge.CodeHash, code) {
		t.Fatal("code not hashed")
	}
	if _, err := s.RequestCode("register", "alice@example.test", 0, "", "ip1"); !errors.Is(err, ErrMailRate) {
		t.Fatal("resend cooldown not enforced")
	}
	if err := s.Reset("alice@example.test", "new-password-123", id, code); !errors.Is(err, ErrMailCode) {
		t.Fatal("cross-purpose code accepted")
	}
	u, err := s.Register("Alice", "alice@example.test", "password-123", id, code)
	if err != nil {
		t.Fatal(err)
	}
	if !u.EmailVerified || u.Status != model.USER_STATUS_PENDING {
		t.Fatal("verified registration bypassed approval")
	}
	if _, err := s.Register("another", "alice@example.test", "password-123", id, code); !errors.Is(err, ErrMailCode) {
		t.Fatal("registration code replayed")
	}
	if err := AllService.UserService.ReviewRegistration(u.Id, true); err != nil {
		t.Fatal(err)
	}
	var notifications int64
	db.Model(&model.MailMessage{}).Where("challenge_id = ?", "").Count(&notifications)
	if notifications != 1 {
		t.Fatal("missing review notification")
	}
	if err := AllService.UserService.ReviewRegistration(u.Id, true); err == nil {
		t.Fatal("duplicate review accepted")
	}
	clearMailRates(t, db)
	rid, err := s.RequestCode("reset", u.Email, 0, "", "ip1")
	if err != nil {
		t.Fatal(err)
	}
	rcode := queuedCode(t, db, rid)
	db.Create(&model.UserToken{UserId: u.Id, Token: "existing-session"})
	if err := s.Reset(u.Email, "new-password-123", rid, rcode); err != nil {
		t.Fatal(err)
	}
	var tokens int64
	db.Model(&model.UserToken{}).Where("user_id = ?", u.Id).Count(&tokens)
	if tokens != 0 {
		t.Fatal("sessions survived reset")
	}
	db.First(u, u.Id)
	ok, _, err := utils.VerifyPassword(u.Password, "new-password-123")
	if err != nil || !ok {
		t.Fatal("password not changed")
	}
	if err := s.Reset(u.Email, "other-password-123", rid, rcode); !errors.Is(err, ErrMailCode) {
		t.Fatal("reset code replayed")
	}
	var before, after int64
	db.Model(&model.MailMessage{}).Count(&before)
	unknown, err := s.RequestCode("reset", "unknown@example.test", 0, "", "ip2")
	if err != nil || len(unknown) != 64 {
		t.Fatal("unknown email disclosed")
	}
	db.Model(&model.MailMessage{}).Count(&after)
	if before != after {
		t.Fatal("unknown recovery sent mail")
	}
}

func TestLegacyBindingAndEmailChange(t *testing.T) {
	s, db := mailFixture(t)
	u := &model.User{Username: "legacy", Email: "legacy@example.test", Password: "password-123", Status: model.COMMON_STATUS_ENABLE}
	if err := AllService.UserService.Create(u); err != nil {
		t.Fatal(err)
	}
	id, err := s.RequestCode("reset", u.Email, 0, "", "ip")
	if err != nil || len(id) != 64 {
		t.Fatal(err)
	}
	var count int64
	db.Model(&model.MailMessage{}).Count(&count)
	if count != 0 {
		t.Fatal("unverified legacy mailbox permitted recovery")
	}
	clearMailRates(t, db)
	if _, err := s.RequestCode("bind", u.Email, u.Id, "wrong", "ip"); !errors.Is(err, ErrMailPassword) {
		t.Fatal("binding did not require password")
	}
	clearMailRates(t, db)
	id, err = s.RequestCode("bind", u.Email, u.Id, "password-123", "ip")
	if err != nil {
		t.Fatal(err)
	}
	code := queuedCode(t, db, id)
	if err := s.Bind(u.Id+1, u.Email, "password-123", id, code); !errors.Is(err, ErrMailCode) {
		t.Fatal("binding code accepted for another user")
	}
	if err := s.Bind(u.Id, u.Email, "password-123", id, code); err != nil {
		t.Fatal(err)
	}
	db.First(u, u.Id)
	if !u.EmailVerified {
		t.Fatal("legacy email not verified")
	}
	clearMailRates(t, db)
	resetID, err := s.RequestCode("reset", u.Email, 0, "", "ip")
	if err != nil {
		t.Fatal(err)
	}
	resetCode := queuedCode(t, db, resetID)
	id, err = s.RequestCode("bind", "new@example.test", u.Id, "password-123", "ip")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Bind(u.Id, "new@example.test", "password-123", id, queuedCode(t, db, id)); err != nil {
		t.Fatal(err)
	}
	if err := s.Reset(u.Email, "new-password-123", resetID, resetCode); !errors.Is(err, ErrMailCode) {
		t.Fatal("old email retained recovery power")
	}
	db.Model(&model.EmailIdentity{}).Where("email = ?", u.Email).Count(&count)
	if count != 0 {
		t.Fatal("old identity retained")
	}
	db.First(u, u.Id)
	u.Email = "admin-edited@example.test"
	if err := AllService.UserService.Update(u); err != nil {
		t.Fatal(err)
	}
	db.First(u, u.Id)
	if u.EmailVerified {
		t.Fatal("admin edit retained verification")
	}
	db.Model(&model.EmailIdentity{}).Where("user_id = ?", u.Id).Count(&count)
	if count != 0 {
		t.Fatal("admin edit retained mailbox claim")
	}
}

func TestMailCodeLimitsAndConcurrency(t *testing.T) {
	s, db := mailFixture(t)
	id, err := s.RequestCode("register", "tries@example.test", 0, "", "ip")
	if err != nil {
		t.Fatal(err)
	}
	code := queuedCode(t, db, id)
	for i := 0; i < 5; i++ {
		if _, err := s.Register("tries", "tries@example.test", "password-123", id, "invalid!"); !errors.Is(err, ErrMailCode) {
			t.Fatal("bad code accepted")
		}
	}
	if _, err := s.Register("tries", "tries@example.test", "password-123", id, code); !errors.Is(err, ErrMailCode) {
		t.Fatal("attempt limit not enforced")
	}
	id, err = s.RequestCode("register", "expired@example.test", 0, "", "ip")
	if err != nil {
		t.Fatal(err)
	}
	code = queuedCode(t, db, id)
	db.Model(&model.EmailChallenge{}).Where("id = ?", id).Update("expires_at", time.Now().Add(-time.Second))
	if _, err := s.Register("expired", "expired@example.test", "password-123", id, code); !errors.Is(err, ErrMailCode) {
		t.Fatal("expired code accepted")
	}
	id, err = s.RequestCode("register", "race@example.test", 0, "", "ip")
	if err != nil {
		t.Fatal(err)
	}
	code = queuedCode(t, db, id)
	var successes atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := s.Register("race", "race@example.test", "password-123", id, code); err == nil {
				successes.Add(1)
			}
		}()
	}
	wg.Wait()
	if successes.Load() != 1 {
		t.Fatalf("concurrent code use: %d successes", successes.Load())
	}
}

func TestMailQueueRetryRestartAndRedaction(t *testing.T) {
	s, db := mailFixture(t)
	id, err := s.RequestCode("register", "delivery@example.test", 0, "", "ip")
	if err != nil {
		t.Fatal(err)
	}
	code := queuedCode(t, db, id)
	s.Send = func(context.Context, model.MailMessage) error { return errors.New("SMTP secret response") }
	if err := s.DeliverOne(context.Background()); err != nil {
		t.Fatal(err)
	}
	var msg model.MailMessage
	db.First(&msg, "challenge_id = ?", id)
	if msg.State != "queued" || msg.Attempts != 1 || msg.ErrorCode != "smtp_delivery_failed" {
		t.Fatal("retry not persisted")
	}
	db.Model(&msg).Update("available_at", time.Now().Add(-time.Second))
	restarted := NewMailService(db, s.cfg)
	restarted.Send = func(_ context.Context, m model.MailMessage) error {
		if !strings.Contains(m.Body, code) {
			t.Fatal("queued payload lost on restart")
		}
		return nil
	}
	if err := restarted.DeliverOne(context.Background()); err != nil {
		t.Fatal(err)
	}
	db.First(&msg, msg.ID)
	if msg.State != "sent" || msg.Body != "" || msg.ErrorCode != "" {
		t.Fatal("delivered code retained in queue")
	}
	if _, err := s.Register("delivery", "delivery@example.test", "password-123", id, code); err != nil {
		t.Fatal("delivery removed verification state")
	}
}
