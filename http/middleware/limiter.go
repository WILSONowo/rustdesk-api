package middleware

import (
	"github.com/gin-gonic/gin"
	"github.com/lejianwen/rustdesk-api/v2/global"
	"github.com/lejianwen/rustdesk-api/v2/http/response"
	"net/http"
)

func Limiter() gin.HandlerFunc {
	return func(c *gin.Context) {
		// Password throttling must not interrupt existing sessions behind the same NAT.
		if c.Request.Method != http.MethodPost || (c.Request.URL.Path != "/api/login" && c.Request.URL.Path != "/api/admin/login") {
			c.Next()
			return
		}
		loginLimiter := global.LoginLimiter
		clientIp := c.ClientIP()
		banned, _ := loginLimiter.CheckSecurityStatus(clientIp)
		if banned {
			if c.Request.URL.Path == "/api/login" {
				c.JSON(http.StatusTooManyRequests, gin.H{"error": response.TranslateMsg(c, "Banned")})
			} else {
				c.JSON(http.StatusTooManyRequests, response.Response{Code: http.StatusLocked, Message: response.TranslateMsg(c, "Banned")})
			}
			c.Abort()
			return
		}
		c.Next()
	}
}
