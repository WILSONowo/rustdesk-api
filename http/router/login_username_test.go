package router

import (
	"encoding/json"
	"testing"

	"github.com/lejianwen/rustdesk-api/v2/global"
	"github.com/lejianwen/rustdesk-api/v2/model"
)

func TestLoginUsername(t *testing.T) {
	const pending = "账号正在等待管理员审核，审核通过后才能登录。"
	const invalid = "用户名或密码错误。"
	for _, route := range []string{"/api/admin/login", "/api/login"} {
		t.Run(route, func(t *testing.T) {
			g, db := stabilityRouter(t)
			global.Config.Lang = "zh-CN"
			var imported model.User
			if err := db.Where("username = ?", "member").First(&imported).Error; err != nil {
				t.Fatal(err)
			}
			imported.Id = 0
			imported.Username = "Pending"
			if err := db.Create(&imported).Error; err != nil {
				t.Fatal(err)
			}
			for _, tc := range []struct{ username, password, message string }{
				{" Me MBER ", "test-pass-1", ""},
				{"Pending", "test-pass-1", ""}, // Exact imported name takes precedence.
				{" Pe NDing ", "test-pass-1", pending},
				{"PENDING", "Test-pass-1", invalid},
				{"unknown", "test-pass-1", invalid},
			} {
				body, err := json.Marshal(map[string]interface{}{
					"username": tc.username, "password": tc.password, "deviceInfo": map[string]string{"type": "client"},
				})
				if err != nil {
					t.Fatal(err)
				}
				w := request(t, g, "POST", route, "", string(body))
				var res struct {
					Message     string
					Error       string
					AccessToken string `json:"access_token"`
					Data        struct{ Token string }
				}
				if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil {
					t.Fatal(err)
				}
				msg, token := res.Message, res.Data.Token
				if route == "/api/login" {
					msg, token = res.Error, res.AccessToken
				}
				if tc.message == "" {
					if w.Code != 200 || token == "" {
						t.Errorf("login %q failed (HTTP %d)", tc.username, w.Code)
					}
				} else if msg != tc.message || token != "" {
					t.Errorf("login %q: message %q, want %q; token issued: %v", tc.username, msg, tc.message, token != "")
				}
			}
		})
	}
}
