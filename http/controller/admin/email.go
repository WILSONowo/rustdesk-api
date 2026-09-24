package admin

import (
	"github.com/gin-gonic/gin"
	"github.com/lejianwen/rustdesk-api/v2/global"
	"github.com/lejianwen/rustdesk-api/v2/http/response"
	"github.com/lejianwen/rustdesk-api/v2/service"
)

type Email struct{}
type emailForm struct {
	CaptchaID   string `json:"captcha_id"`
	Captcha     string `json:"captcha"`
	Email       string `json:"email" binding:"required,email,max=254"`
	Password    string `json:"password"`
	ChallengeID string `json:"challenge_id"`
	Code        string `json:"code"`
}

func emailFailure(c *gin.Context, err error) {
	key := err.Error()
	switch key {
	case "MailUnavailable", "MailRateLimited", "MailCodeInvalid", "EmailUnavailable", "EmailInvalid", "OldPasswordError", "RegistrationFailed", "UsernameExists", "RegistrationRateLimited", "CaptchaError", "ParamsError":
	default:
		key = "OperationFailed"
	}
	response.Fail(c, 101, response.TranslateMsg(c, key))
}
func (ct *Email) Options(c *gin.Context) {
	response.Success(c, gin.H{"available": service.AllService.MailService.Ready(), "registration_verification": global.Config.Mail.RequireRegistrationVerification})
}
func (ct *Email) RegisterCode(c *gin.Context) {
	if !global.Config.App.Register {
		response.Fail(c, 101, response.TranslateMsg(c, "RegisterClosed"))
		return
	}
	ct.request(c, "register", false)
}
func (ct *Email) ResetCode(c *gin.Context) { ct.request(c, "reset", false) }
func (ct *Email) BindCode(c *gin.Context)  { ct.request(c, "bind", true) }
func (ct *Email) request(c *gin.Context, purpose string, authenticated bool) {
	var f emailForm
	if c.ShouldBindJSON(&f) != nil {
		response.Fail(c, 101, response.TranslateMsg(c, "ParamsError"))
		return
	}
	var uid uint
	if purpose == "register" {
		if err := service.AllService.RegistrationService.VerifyCaptcha(c.ClientIP(), f.CaptchaID, f.Captcha); err != nil {
			emailFailure(c, err)
			return
		}
		available, err := service.RegistrationAvailable(service.DB, "email", f.Email)
		if err != nil {
			emailFailure(c, err)
			return
		}
		if !available {
			emailFailure(c, service.ErrEmailUsed)
			return
		}
	}
	if authenticated {
		uid = service.AllService.UserService.CurUser(c).Id
	}
	id, err := service.AllService.MailService.RequestCode(purpose, f.Email, uid, f.Password, c.ClientIP())
	if err != nil {
		emailFailure(c, err)
		return
	}
	response.Success(c, gin.H{"challenge_id": id, "retry_after": 60, "message": response.TranslateMsg(c, "MailCodeRequested")})
}
func (ct *Email) Bind(c *gin.Context) {
	var f emailForm
	if c.ShouldBindJSON(&f) != nil || len(f.ChallengeID) != 64 || len(f.Code) != 8 {
		response.Fail(c, 101, response.TranslateMsg(c, "ParamsError"))
		return
	}
	u := service.AllService.UserService.CurUser(c)
	if err := service.AllService.MailService.Bind(u.Id, f.Email, f.Password, f.ChallengeID, f.Code); err != nil {
		emailFailure(c, err)
		return
	}
	response.Success(c, nil)
}
func (ct *Email) Reset(c *gin.Context) {
	var f struct {
		emailForm
		NewPassword     string `json:"new_password" binding:"required,min=8,max=32"`
		ConfirmPassword string `json:"confirm_password" binding:"required,eqfield=NewPassword"`
	}
	if c.ShouldBindJSON(&f) != nil || len(f.ChallengeID) != 64 || len(f.Code) != 8 {
		response.Fail(c, 101, response.TranslateMsg(c, "ParamsError"))
		return
	}
	if err := service.AllService.MailService.Reset(f.Email, f.NewPassword, f.ChallengeID, f.Code); err != nil {
		emailFailure(c, err)
		return
	}
	response.Success(c, nil)
}
