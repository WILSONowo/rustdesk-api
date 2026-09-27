package service

import (
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/lejianwen/rustdesk-api/v2/model"
	"github.com/lejianwen/rustdesk-api/v2/utils"
	"gorm.io/gorm"
)

var ErrRegistrationRate = errors.New("RegistrationRateLimited")
var ErrRegistrationCaptcha = errors.New("CaptchaError")

type registrationCaptcha struct {
	ip, answer string
	expires    time.Time
}

// Registration challenges are separate from login challenges, bound to the
// requesting IP and consumed even on a wrong answer. Restarting fails closed.
type RegistrationService struct {
	db       *gorm.DB
	mu       sync.Mutex
	captchas map[string]registrationCaptcha
	provider utils.CaptchaProvider
}

func NewRegistrationService(db *gorm.DB, provider utils.CaptchaProvider) *RegistrationService {
	return &RegistrationService{db: db, captchas: make(map[string]registrationCaptcha), provider: provider}
}

func (s *RegistrationService) Limit(ip, action string, limit int) error {
	err := s.db.Transaction(func(tx *gorm.DB) error {
		return mailQuota(tx, "registration:"+action+":"+ip, limit, 10*time.Minute)
	})
	if errors.Is(err, ErrMailRate) {
		return ErrRegistrationRate
	}
	return err
}

func (s *RegistrationService) Captcha(ip string) (string, string, error) {
	if err := s.Limit(ip, "captcha", 30); err != nil {
		return "", "", err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, c := range s.captchas {
		if time.Now().After(c.expires) {
			delete(s.captchas, id)
		}
	}
	if len(s.captchas) >= 4096 {
		return "", "", ErrRegistrationRate
	}
	_, content, answer, err := s.provider.Generate()
	if err != nil {
		return "", "", err
	}
	b64, err := s.provider.Draw(content)
	if err != nil {
		return "", "", err
	}
	id := randomID()
	s.captchas[id] = registrationCaptcha{digest(ip), strings.ToLower(answer), time.Now().Add(s.provider.Expiration())}
	return id, b64, nil
}

func (s *RegistrationService) VerifyCaptcha(ip, id, answer string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	c, ok := s.captchas[id]
	delete(s.captchas, id)
	if !ok || c.ip != digest(ip) || time.Now().After(c.expires) || c.answer != strings.ToLower(strings.TrimSpace(answer)) {
		return ErrRegistrationCaptcha
	}
	return nil
}

// Checks include pending, enabled and disabled accounts. No account status is
// returned to anonymous callers, and the final insert still enforces uniqueness.
func RegistrationAvailable(tx *gorm.DB, field, value string) (bool, error) {
	switch field {
	case "username":
		value = (&UserService{}).formatUsername(value)
		if len([]rune(value)) < 2 || len([]rune(value)) > 32 {
			return false, errors.New("ParamsError")
		}
		var n int64
		if err := tx.Model(&model.User{}).Where("LOWER(username) = ?", value).Count(&n).Error; err != nil {
			return false, err
		}
		return n == 0 && !AllService.LdapService.IsUsernameExists(value), nil
	case "email":
		var err error
		value, err = NormalizeEmail(value)
		if err != nil {
			return false, err
		}
		return emailAvailable(tx, value, 0) && !AllService.UserService.IsEmailExistsLdap(value), nil
	default:
		return false, errors.New("ParamsError")
	}
}

func checkRegistrationIdentity(tx *gorm.DB, username, email string) error {
	for _, item := range []struct{ field, value, message string }{
		{"username", username, "UsernameExists"}, {"email", email, "EmailUnavailable"},
	} {
		if item.field == "email" && email == "" {
			continue
		}
		ok, err := RegistrationAvailable(tx, item.field, item.value)
		if err != nil {
			return err
		}
		if !ok {
			return errors.New(item.message)
		}
	}
	return nil
}
