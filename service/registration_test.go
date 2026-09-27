package service

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/lejianwen/rustdesk-api/v2/model"
)

func TestRegistrationCaptchaAndQuota(t *testing.T) {
	_, db := mailFixture(t)
	s := AllService.RegistrationService
	newCaptcha := func() (string, string) {
		id, image, err := s.Captcha("ip")
		if err != nil || !strings.HasPrefix(image, "data:image/png;base64,") {
			t.Fatalf("captcha generation: %v", err)
		}
		return id, s.captchas[id].answer
	}
	id, answer := newCaptcha()
	if s.VerifyCaptcha("ip", id, "wrong") == nil || s.VerifyCaptcha("ip", id, answer) == nil {
		t.Fatal("incorrect attempt did not consume captcha")
	}
	id, answer = newCaptcha()
	if s.VerifyCaptcha("another-ip", id, answer) == nil {
		t.Fatal("captcha accepted from another IP")
	}
	id, answer = newCaptcha()
	if err := s.VerifyCaptcha("ip", id, strings.ToUpper(answer)); err != nil {
		t.Fatal(err)
	}
	if s.VerifyCaptcha("ip", id, answer) == nil {
		t.Fatal("captcha replay accepted")
	}
	id, answer = newCaptcha()
	c := s.captchas[id]
	c.expires = time.Now().Add(-time.Second)
	s.captchas[id] = c
	if s.VerifyCaptcha("ip", id, answer) == nil {
		t.Fatal("expired captcha accepted")
	}
	for i := 0; i < 3; i++ {
		if err := s.Limit("quota-ip", "availability", 3); err != nil {
			t.Fatal(err)
		}
	}
	if !errors.Is(s.Limit("quota-ip", "availability", 3), ErrRegistrationRate) {
		t.Fatal("quota not enforced")
	}
	restarted := NewRegistrationService(db, s.provider)
	if !errors.Is(restarted.Limit("quota-ip", "availability", 3), ErrRegistrationRate) {
		t.Fatal("restart cleared quota")
	}
}

func TestPendingIdentityAndAdminNotification(t *testing.T) {
	s, db := mailFixture(t)
	admin := true
	for _, u := range []model.User{
		{Username: "reviewer", Email: "admin@example.test", EmailVerified: true, IsAdmin: &admin, Status: model.COMMON_STATUS_ENABLE},
		{Username: "unverified", Email: "unverified@example.test", IsAdmin: &admin, Status: model.COMMON_STATUS_ENABLE},
		{Username: "disabled", Email: "disabled@example.test", EmailVerified: true, IsAdmin: &admin, Status: model.COMMON_STATUS_DISABLED},
	} {
		if err := db.Create(&u).Error; err != nil {
			t.Fatal(err)
		}
	}
	id, err := s.RequestCode("register", "pending@example.test", 0, "", "ip")
	if err != nil {
		t.Fatal(err)
	}
	code := queuedCode(t, db, id)
	u, err := s.Register("Pending", "PENDING@example.test", "password-123", id, code)
	if err != nil {
		t.Fatal(err)
	}
	for field, value := range map[string]string{"username": "PENDING", "email": "PENDING@example.test"} {
		if ok, err := RegistrationAvailable(db, field, value); err != nil || ok {
			t.Fatalf("pending %s not reserved: %v", field, err)
		}
	}
	var mail []model.MailMessage
	db.Where("subject = ?", "RustDesk - 新用户待审核").Find(&mail)
	if len(mail) != 1 || mail[0].To != "admin@example.test" || !strings.Contains(mail[0].Body, "pending@example.test") || !strings.Contains(mail[0].Body, "https://remote.neko-arc.top/_admin/#/user/index") {
		t.Fatal("incorrect administrator notification")
	}
	if _, err := s.Register("Pending", u.Email, "password-123", id, code); err == nil {
		t.Fatal("registration replay accepted")
	}
	if AllService.UserService.Register("another", u.Email, "password-123") != nil {
		t.Fatal("legacy path reused pending email")
	}
	if AllService.UserService.Register(u.Username, "another@example.test", "password-123") != nil {
		t.Fatal("legacy path reused pending username")
	}
	var count int64
	db.Model(&model.MailMessage{}).Where("subject = ?", "RustDesk - 新用户待审核").Count(&count)
	if count != 1 {
		t.Fatal("duplicate registration created extra mail")
	}
	if err := AllService.UserService.ReviewRegistration(u.Id, true); err != nil {
		t.Fatal(err)
	}
	db.Model(&model.MailMessage{}).Where("subject = ? AND `to` = ?", "RustDesk - 账号审核结果", u.Email).Count(&count)
	if count != 1 {
		t.Fatal("approval notification regressed")
	}
	delivered := make(map[string]int)
	s.Send = func(_ context.Context, msg model.MailMessage) error {
		delivered[msg.To+":"+msg.Subject]++
		return nil
	}
	// The consumed registration code is skipped; both notification types must
	// still reach the same delivery worker without needing a challenge record.
	for i := 0; i < 3; i++ {
		if err := s.DeliverOne(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	if delivered["admin@example.test:RustDesk - 新用户待审核"] != 1 || delivered[u.Email+":RustDesk - 账号审核结果"] != 1 {
		t.Fatal("notification delivery did not reach both recipients")
	}
}
