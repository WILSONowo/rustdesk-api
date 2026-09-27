package service

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"math/big"
	"net/mail"
	"strings"
	"sync"
	"time"

	"github.com/lejianwen/rustdesk-api/v2/config"
	"github.com/lejianwen/rustdesk-api/v2/model"
	"github.com/lejianwen/rustdesk-api/v2/utils"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var ErrMailUnavailable = errors.New("MailUnavailable")
var ErrMailRate = errors.New("MailRateLimited")
var ErrMailCode = errors.New("MailCodeInvalid")
var ErrEmailUsed = errors.New("EmailUnavailable")
var ErrMailPassword = errors.New("OldPasswordError")

type MailService struct {
	db   *gorm.DB
	cfg  config.Mail
	mu   sync.Mutex
	Send func(context.Context, model.MailMessage) error
}

func NewMailService(db *gorm.DB, cfg config.Mail) *MailService {
	if cfg.SendIntervalSeconds < 1 {
		cfg.SendIntervalSeconds = 5
	}
	if cfg.DailyLimit < 1 {
		cfg.DailyLimit = 100
	}
	s := &MailService{db: db, cfg: cfg}
	s.Send = s.sendSMTP
	return s
}

func NormalizeEmail(value string) (string, error) {
	value = strings.ToLower(strings.TrimSpace(value))
	a, err := mail.ParseAddress(value)
	if err != nil || a.Address != value || len(value) > 254 || strings.ContainsAny(value, "\r\n") {
		return "", errors.New("EmailInvalid")
	}
	return value, nil
}

func (s *MailService) Ready() bool {
	_, err := NormalizeEmail(s.cfg.From)
	return s.cfg.Enabled && s.cfg.Host != "" && s.cfg.Port > 0 && s.cfg.Port <= 65535 && err == nil &&
		(s.cfg.TLS == "implicit" || s.cfg.TLS == "starttls" || s.cfg.TLS == "none") &&
		(s.cfg.Username == "" || s.cfg.Password != "")
}

func randomID() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}
func digest(value string) string { h := sha256.Sum256([]byte(value)); return hex.EncodeToString(h[:]) }

func mailQuota(tx *gorm.DB, key string, limit int, window time.Duration) error {
	now := time.Now()
	key = digest(key)
	if err := tx.Where("key = ? AND expires_at <= ?", key, now).Delete(&model.MailRate{}).Error; err != nil {
		return err
	}
	r := model.MailRate{Key: key, Count: 1, ExpiresAt: now.Add(window)}
	if err := tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "key"}}, DoUpdates: clause.Assignments(map[string]interface{}{"count": gorm.Expr("count + 1")})}).Create(&r).Error; err != nil {
		return err
	}
	if err := tx.First(&r, "key = ?", key).Error; err != nil {
		return err
	}
	if r.Count > limit {
		return ErrMailRate
	}
	return nil
}

func emailAvailable(tx *gorm.DB, email string, userID uint) bool {
	var n int64
	if tx.Model(&model.User{}).Where("LOWER(email) = ? AND id <> ?", email, userID).Count(&n).Error != nil || n > 0 {
		return false
	}
	if tx.Model(&model.EmailIdentity{}).Where("email = ? AND user_id <> ?", email, userID).Count(&n).Error != nil || n > 0 {
		return false
	}
	return true
}

// Unknown/unverified recovery recipients and duplicate registrations receive the
// same response and quotas, without disclosing whether the account exists.
func (s *MailService) RequestCode(purpose, email string, userID uint, password, ip string) (string, error) {
	if !s.Ready() {
		return "", ErrMailUnavailable
	}
	email, err := NormalizeEmail(email)
	if err != nil {
		return "", err
	}
	if purpose != "register" && purpose != "bind" && purpose != "reset" {
		return "", ErrMailCode
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	id := randomID()
	err = s.db.Transaction(func(tx *gorm.DB) error {
		if err := mailQuota(tx, "recipient-minute:"+email, 1, time.Minute); err != nil {
			return err
		}
		if err := mailQuota(tx, "recipient-hour:"+email, 5, time.Hour); err != nil {
			return err
		}
		return mailQuota(tx, "ip-hour:"+ip, 60, time.Hour)
	})
	if err != nil {
		return "", err
	}
	err = s.db.Transaction(func(tx *gorm.DB) error {
		var user model.User
		eligible := true
		switch purpose {
		case "register":
			eligible = emailAvailable(tx, email, 0)
		case "bind":
			if tx.First(&user, userID).Error != nil {
				return ErrMailPassword
			}
			ok, _, e := utils.VerifyPassword(user.Password, password)
			if e != nil || !ok {
				return ErrMailPassword
			}
			if !emailAvailable(tx, email, userID) {
				return ErrEmailUsed
			}
		case "reset":
			var identity model.EmailIdentity
			eligible = tx.First(&identity, "email = ?", email).Error == nil && tx.First(&user, identity.UserID).Error == nil && user.EmailVerified && user.Email == email && user.Status == model.COMMON_STATUS_ENABLE
			userID = user.Id
		}
		if !eligible {
			return nil
		}
		if err := mailQuota(tx, "global-day", s.cfg.DailyLimit, 24*time.Hour); err != nil {
			return err
		}
		var count int64
		if err := tx.Model(&model.MailMessage{}).Where("state IN ?", []string{"queued", "sending"}).Count(&count).Error; err != nil {
			return err
		}
		if count >= 1000 {
			return ErrMailRate
		}
		n, err := rand.Int(rand.Reader, big.NewInt(100000000))
		if err != nil {
			return err
		}
		code := fmt.Sprintf("%08d", n.Int64())
		now := time.Now()
		ch := model.EmailChallenge{ID: id, Purpose: purpose, Email: email, UserID: userID, CodeHash: digest(id + ":" + code), CredentialHash: digest(user.Password), CreatedAt: now, ExpiresAt: now.Add(10 * time.Minute)}
		if err := tx.Model(&model.EmailChallenge{}).Where("email = ? AND purpose = ? AND user_id = ?", email, purpose, userID).Update("consumed", true).Error; err != nil {
			return err
		}
		if err := tx.Create(&ch).Error; err != nil {
			return err
		}
		label := map[string]string{"register": "注册邮箱验证", "bind": "绑定或更换邮箱", "reset": "重置密码"}[purpose]
		body := fmt.Sprintf("您正在进行：%s\n\n验证码：%s\n有效期为 10 分钟，请勿转发给他人。\n如非本人操作，请忽略本邮件。\n\n%s\n", label, code, s.cfg.PublicURL)
		return s.enqueue(tx, email, "RustDesk - "+label, body, id, ch.ExpiresAt)
	})
	return id, err
}

func (s *MailService) enqueue(tx *gorm.DB, to, subject, body, challenge string, expires time.Time) error {
	return tx.Create(&model.MailMessage{To: to, Subject: subject, Body: body, ChallengeID: challenge, State: "queued", AvailableAt: time.Now(), ExpiresAt: expires}).Error
}

// Commit invalid attempt counts even when verification is refused. The claim and
// its effect share one transaction; a failed effect leaves the code reusable.
func (s *MailService) consume(id, code, purpose, email string, userID uint, apply func(*gorm.DB, *model.EmailChallenge) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	var refused error
	err := s.db.Transaction(func(tx *gorm.DB) error {
		var ch model.EmailChallenge
		if tx.First(&ch, "id = ?", id).Error != nil || ch.Consumed || !time.Now().Before(ch.ExpiresAt) || ch.Attempts >= 5 || ch.Purpose != purpose || ch.Email != email || (purpose == "bind" && ch.UserID != userID) {
			refused = ErrMailCode
			return nil
		}
		if subtle.ConstantTimeCompare([]byte(ch.CodeHash), []byte(digest(id+":"+code))) != 1 {
			refused = ErrMailCode
			return tx.Model(&ch).Update("attempts", gorm.Expr("attempts + 1")).Error
		}
		claim := tx.Model(&model.EmailChallenge{}).Where("id = ? AND consumed = ? AND attempts < ? AND expires_at > ?", id, false, 5, time.Now()).Update("consumed", true)
		if claim.Error != nil {
			return claim.Error
		}
		if claim.RowsAffected != 1 {
			return ErrMailCode
		}
		return apply(tx, &ch)
	})
	if err != nil {
		return err
	}
	return refused
}

func (s *MailService) Register(username, email, password, id, code string) (*model.User, error) {
	email, err := NormalizeEmail(email)
	if err != nil {
		return nil, err
	}
	username = (&UserService{}).formatUsername(username)
	if len([]rune(username)) < 2 {
		return nil, errors.New("RegistrationFailed")
	}
	u := &model.User{Username: username, Email: email, EmailVerified: true, GroupId: 1, Status: model.USER_STATUS_PENDING}
	err = s.consume(id, code, "register", email, 0, func(tx *gorm.DB, ch *model.EmailChallenge) error {
		if err := checkRegistrationIdentity(tx, username, email); err != nil {
			return err
		}
		hash, err := utils.EncryptPassword(password)
		if err != nil {
			return err
		}
		u.Password = hash
		if err := tx.Create(u).Error; err != nil {
			return errors.New("RegistrationFailed")
		}
		if err := tx.Create(&model.EmailIdentity{Email: email, UserID: u.Id}).Error; err != nil {
			return err
		}
		return s.QueuePendingReview(tx, u)
	})
	return u, err
}

func (s *MailService) Bind(userID uint, email, password, id, code string) error {
	email, err := NormalizeEmail(email)
	if err != nil {
		return err
	}
	if err := s.db.Transaction(func(tx *gorm.DB) error { return mailQuota(tx, fmt.Sprintf("bind-confirm:%d", userID), 20, time.Hour) }); err != nil {
		return err
	}
	return s.consume(id, code, "bind", email, userID, func(tx *gorm.DB, ch *model.EmailChallenge) error {
		var u model.User
		if tx.First(&u, userID).Error != nil {
			return ErrMailPassword
		}
		ok, _, err := utils.VerifyPassword(u.Password, password)
		if err != nil || !ok || digest(u.Password) != ch.CredentialHash {
			return ErrMailPassword
		}
		if !emailAvailable(tx, email, userID) {
			return ErrEmailUsed
		}
		oldEmail, wasVerified := u.Email, u.EmailVerified
		if err := tx.Where("user_id = ?", userID).Delete(&model.EmailIdentity{}).Error; err != nil {
			return err
		}
		if err := tx.Create(&model.EmailIdentity{Email: email, UserID: userID}).Error; err != nil {
			return ErrEmailUsed
		}
		if err := tx.Model(&u).Updates(map[string]interface{}{"email": email, "email_verified": true}).Error; err != nil {
			return err
		}
		if err := tx.Model(&model.EmailChallenge{}).Where("user_id = ?", userID).Update("consumed", true).Error; err != nil {
			return err
		}
		if wasVerified && oldEmail != email && s.Ready() {
			return s.enqueue(tx, oldEmail, "RustDesk - 邮箱已变更", "您的账号绑定邮箱已变更。如非本人操作，请联系管理员。", "", time.Now().Add(24*time.Hour))
		}
		return nil
	})
}

func (s *MailService) Reset(email, password, id, code string) error {
	email, err := NormalizeEmail(email)
	if err != nil {
		return err
	}
	return s.consume(id, code, "reset", email, 0, func(tx *gorm.DB, ch *model.EmailChallenge) error {
		var u model.User
		var identity model.EmailIdentity
		if tx.First(&u, ch.UserID).Error != nil || !u.EmailVerified || u.Email != email || u.Status != model.COMMON_STATUS_ENABLE || digest(u.Password) != ch.CredentialHash || tx.First(&identity, "email = ? AND user_id = ?", email, u.Id).Error != nil {
			return ErrMailCode
		}
		hash, err := utils.EncryptPassword(password)
		if err != nil {
			return err
		}
		if err := tx.Model(&u).Update("password", hash).Error; err != nil {
			return err
		}
		if err := tx.Where("user_id = ?", u.Id).Delete(&model.UserToken{}).Error; err != nil {
			return err
		}
		return tx.Model(&model.EmailChallenge{}).Where("user_id = ?", u.Id).Update("consumed", true).Error
	})
}

func (s *MailService) QueueReview(tx *gorm.DB, u *model.User, approve bool) error {
	if !s.Ready() || !u.EmailVerified {
		return nil
	}
	decision := "您的注册申请未通过审核。如有疑问，请联系管理员。"
	if approve {
		decision = "您的注册申请已通过审核，可以使用账号登录网页和 RustDesk 客户端。"
	}
	return s.enqueue(tx, u.Email, "RustDesk - 账号审核结果", decision+"\n\n"+s.cfg.PublicURL+"/_admin/", "", time.Now().Add(48*time.Hour))
}

// Queue once in the registration transaction: retries cannot create a second
// account or notification. SMTP delivery runs asynchronously after commit.
func (s *MailService) QueuePendingReview(tx *gorm.DB, u *model.User) error {
	if !s.Ready() {
		return nil
	}
	var admins []model.User
	if err := tx.Where("is_admin = ? AND status = ? AND email_verified = ?", true, model.COMMON_STATUS_ENABLE, true).Find(&admins).Error; err != nil {
		return err
	}
	seen := make(map[string]bool)
	for _, admin := range admins {
		email, err := NormalizeEmail(admin.Email)
		if err != nil || seen[email] {
			continue
		}
		seen[email] = true
		body := fmt.Sprintf("有新的注册申请等待审核。\n\n用户名：%s\n邮箱：%s\n\n请登录管理员账号后，在用户管理中筛选“待审核”：\n%s/_admin/#/user/index\n\n审核前，该账号无法登录。", u.Username, u.Email, strings.TrimRight(s.cfg.PublicURL, "/"))
		if err := s.enqueue(tx, email, "RustDesk - 新用户待审核", body, "", time.Now().Add(48*time.Hour)); err != nil {
			return err
		}
	}
	return nil
}
