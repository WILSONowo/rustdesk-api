package middleware

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

// AccountsOrigin preserves the account-only deployment boundaries when the CDN
// connects directly to the application without a local reverse proxy.
func AccountsOrigin() gin.HandlerFunc {
	return func(c *gin.Context) {
		path := c.Request.URL.Path
		if strings.HasPrefix(path, "/api/") {
			// Authenticated GET responses must never become shared CDN objects.
			c.Header("Cache-Control", "no-store, private")
			c.Header("Pragma", "no-cache")
			if path == "/api/heartbeat" || strings.HasPrefix(path, "/api/sysinfo") || strings.HasPrefix(path, "/api/audit/") {
				c.AbortWithStatus(http.StatusNotFound)
				return
			}
			const maxBody = 2 << 20
			if c.Request.ContentLength > maxBody {
				c.AbortWithStatus(http.StatusRequestEntityTooLarge)
				return
			}
			c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxBody)
		}
		c.Next()
	}
}
