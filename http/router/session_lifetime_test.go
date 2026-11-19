package router

import (
	"encoding/json"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/lejianwen/rustdesk-api/v2/global"
	api "github.com/lejianwen/rustdesk-api/v2/http/controller/api"
	"github.com/lejianwen/rustdesk-api/v2/http/middleware"
	"github.com/lejianwen/rustdesk-api/v2/lib/jwt"
	"github.com/lejianwen/rustdesk-api/v2/model"
	"github.com/lejianwen/rustdesk-api/v2/service"
)

func TestAccountSessionLifetime(t *testing.T) {
	for _, key := range []string{"", "test-signing-key"} {
		t.Run(map[bool]string{true: "jwt", false: "opaque"}[key != ""], func(t *testing.T) {
			g, db := stabilityRouter(t)
			// An already elapsed JWT duration makes a fixed expiry immediately observable.
			previous := global.Jwt
			global.Jwt = jwt.NewJwt(key, -time.Minute)
			service.Jwt = global.Jwt
			t.Cleanup(func() { global.Jwt = previous; service.Jwt = nil })
			global.Config.App.TokenExpire = 7 * 24 * time.Hour
			nativeAPI := g.Group("/api", middleware.RustAuth())
			nativeAPI.POST("/currentUser", (&api.User{}).Info)
			nativeAPI.POST("/logout", (&api.Login{}).Logout)
			g.GET("/api/oidc/auth-query", (&api.Oauth{}).OidcAuthQuery)
			fetch := func(path, token string) int {
				w := httptest.NewRecorder()
				req := httptest.NewRequest("POST", path, nil)
				req.Header.Set("Authorization", "Bearer "+token)
				g.ServeHTTP(w, req)
				return w.Code
			}
			readToken := func(w *httptest.ResponseRecorder) string {
				t.Helper()
				var body struct {
					Token string `json:"access_token"`
				}
				if json.Unmarshal(w.Body.Bytes(), &body) != nil || body.Token == "" {
					t.Fatal("missing native token", w.Code)
				}
				return body.Token
			}
			login := func() string {
				return readToken(request(t, g, "POST", "/api/login", "", "{\"username\":\"member\",\"password\":\"test-pass-1\",\"deviceInfo\":{\"type\":\"client\"}}"))
			}
			token := login()
			service.AllService.OauthService.SetOauthCache("native-test", &service.OauthCacheItem{UserId: 2, Action: service.OauthActionTypeLogin, DeviceType: "client"}, 0)
			second := readToken(request(t, g, "GET", "/api/oidc/auth-query?code=native-test", "", ""))
			if token == second || fetch("/api/currentUser", token) != 200 || fetch("/api/currentUser", second) != 200 {
				t.Fatal("native sessions must work independently")
			}
			var row model.UserToken
			if db.Where("token = ?", token).First(&row).Error != nil || row.ExpiredAt != model.NativeSessionNoExpiry {
				t.Fatal("native session gained an expiry")
			}
			if user, _ := service.AllService.UserService.InfoByAccessToken(token); user.Id != 0 {
				t.Fatal("native token accepted for web admin")
			}
			if err := service.AllService.UserService.DeleteToken(&row); err != nil {
				t.Fatal(err)
			}
			if fetch("/api/currentUser", token) != 401 || fetch("/api/currentUser", second) != 200 {
				t.Fatal("per-device revocation failed")
			}
			if fetch("/api/logout", second) != 200 || fetch("/api/currentUser", second) != 401 {
				t.Fatal("logout did not revoke token")
			}
			web := responseData(t, request(t, g, "POST", "/api/admin/login", "", "{\"username\":\"member\",\"password\":\"test-pass-1\"}"))["token"].(string)
			user, session := service.AllService.UserService.InfoByAccessToken(web)
			if user.Id != 2 || time.Until(time.Unix(session.ExpiredAt, 0)) < 167*time.Hour || time.Until(time.Unix(session.ExpiredAt, 0)) > 168*time.Hour {
				t.Fatal("web lifetime changed")
			}
			session.ExpiredAt = time.Now().Add(time.Hour).Unix()
			if err := db.Save(session).Error; err != nil {
				t.Fatal(err)
			}
			service.AllService.UserService.AutoRefreshAccessToken(session)
			if time.Until(time.Unix(session.ExpiredAt, 0)) < 167*time.Hour {
				t.Fatal("web session did not renew")
			}
			if err := db.Model(session).Update("expired_at", time.Now().Unix()-1).Error; err != nil {
				t.Fatal(err)
			}
			if u, _ := service.AllService.UserService.InfoByClientToken(web); u.Id != 0 {
				t.Fatal("expired session reactivated")
			}
			third := login()
			user.Status = model.COMMON_STATUS_DISABLED
			if err := service.AllService.UserService.Update(user); err != nil {
				t.Fatal(err)
			}
			user.Status = model.COMMON_STATUS_ENABLE
			if err := service.AllService.UserService.Update(user); err != nil {
				t.Fatal(err)
			}
			if fetch("/api/currentUser", third) != 401 {
				t.Fatal("disabled account session survived re-enablement")
			}
		})
	}
}
