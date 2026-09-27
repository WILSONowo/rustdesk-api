package admin

import (
	"github.com/gin-gonic/gin"
	"github.com/lejianwen/rustdesk-api/v2/global"
	"github.com/lejianwen/rustdesk-api/v2/http/response"
	"github.com/lejianwen/rustdesk-api/v2/service"
	"net/http"
)

// Apply before parsing request bodies or checking identities. All registration
// endpoints fail closed when disabled; availability must never be CDN-cached.
func RegistrationLimit(action string, limit int) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("Cache-Control", "private, no-store")
		if !global.Config.App.Register {
			response.Fail(c, 101, response.TranslateMsg(c, "RegisterClosed"))
			c.Abort()
			return
		}
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 8192)
		if err := service.AllService.RegistrationService.Limit(c.ClientIP(), action, limit); err != nil {
			emailFailure(c, err)
			c.Abort()
			return
		}
		c.Next()
	}
}

func (*User) RegistrationCaptcha(c *gin.Context) {
	id, b64, err := service.AllService.RegistrationService.Captcha(c.ClientIP())
	if err != nil {
		emailFailure(c, err)
		return
	}
	response.Success(c, gin.H{"captcha": gin.H{"id": id, "b64": b64}})
}

func (*User) RegistrationAvailability(c *gin.Context) {
	var f struct {
		Field string `json:"field" binding:"required,oneof=username email"`
		Value string `json:"value" binding:"required,max=254"`
	}
	if c.ShouldBindJSON(&f) != nil {
		response.Fail(c, 101, response.TranslateMsg(c, "ParamsError"))
		return
	}
	available, err := service.RegistrationAvailable(service.DB, f.Field, f.Value)
	if err != nil {
		emailFailure(c, err)
		return
	}
	response.Success(c, gin.H{"available": available})
}
