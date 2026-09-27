package middleware

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestAccountsOriginBoundaries(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(AccountsOrigin())
	r.NoRoute(func(c *gin.Context) {
		if _, err := io.Copy(io.Discard, c.Request.Body); err != nil {
			c.Status(http.StatusRequestEntityTooLarge)
			return
		}
		c.Status(http.StatusOK)
	})
	for _, tc := range []struct {
		path    string
		status  int
		private bool
	}{
		{"/api/admin/user/list", 200, true},
		{"/api/ab", 200, true},
		{"/api/login", 200, true},
		{"/api/heartbeat", 404, true},
		{"/api/sysinfo", 404, true},
		{"/api/sysinfo_ver", 404, true},
		{"/api/audit/conn", 404, true},
		{"/api/audit/file", 404, true},
		{"/_admin/", 200, false},
	} {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest("GET", tc.path, nil))
		if w.Code != tc.status {
			t.Fatalf("%s: status %d", tc.path, w.Code)
		}
		if tc.private && w.Header().Get("Cache-Control") != "no-store, private" {
			t.Fatalf("%s: missing cache protection", tc.path)
		}
	}
	for _, chunked := range []bool{false, true} {
		req := httptest.NewRequest("POST", "/api/ab", strings.NewReader(strings.Repeat("x", (2<<20)+1)))
		if chunked {
			req.ContentLength = -1
		}
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusRequestEntityTooLarge {
			t.Fatalf("large request accepted (chunked=%v): %d", chunked, w.Code)
		}
	}
}
