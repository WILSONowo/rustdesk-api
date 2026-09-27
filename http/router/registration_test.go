package router

import (
	"github.com/lejianwen/rustdesk-api/v2/global"
	"github.com/lejianwen/rustdesk-api/v2/model"
	"github.com/lejianwen/rustdesk-api/v2/service"
	"testing"
	"time"
)

// The full router suite uses a deterministic provider, never a production bypass.
type registrationTestCaptcha struct{}

func (registrationTestCaptcha) Generate() (string, string, string, error) {
	return "", "abcd", "abcd", nil
}
func (registrationTestCaptcha) Expiration() time.Duration   { return time.Minute }
func (registrationTestCaptcha) Draw(string) (string, error) { return "data:image/png;base64,test", nil }

func testRegistrationGuards(t *testing.T, request func(string, string, string, any) (int, map[string]any)) {
	t.Helper()
	var before, after int64
	service.DB.Model(&model.MailMessage{}).Count(&before)
	_, image := request("GET", "/api/admin/user/registration-captcha", "", nil)
	id := image["data"].(map[string]any)["captcha"].(map[string]any)["id"]
	form := map[string]any{"email": "guard@example.test", "captcha_id": id, "captcha": "wrong"}
	_, res := request("POST", "/api/admin/email/register-code", "", form)
	if res["code"] != float64(101) {
		t.Fatal("wrong captcha accepted")
	}
	form["captcha"] = "abcd"
	_, res = request("POST", "/api/admin/email/register-code", "", form)
	if res["code"] != float64(101) {
		t.Fatal("consumed captcha accepted")
	}
	service.DB.Model(&model.MailMessage{}).Count(&after)
	if before != after {
		t.Fatal("invalid captcha sent email")
	}
	service.DB.Where("1 = 1").Delete(&model.MailRate{})
	for i := 0; i < 61; i++ {
		_, res = request("POST", "/api/admin/user/registration-availability", "", map[string]any{"field": "username", "value": "guard-available"})
		if i < 60 && res["code"] != float64(0) {
			t.Fatal("availability stopped before quota")
		}
	}
	if res["code"] != float64(101) || res["message"] != "Too many registration requests. Please try again later." {
		t.Fatal("anonymous enumeration is not rate limited")
	}
	global.Config.App.Register = false
	defer func() { global.Config.App.Register = true }()
	for _, route := range []struct{ method, path string }{
		{"GET", "/api/admin/user/registration-captcha"}, {"POST", "/api/admin/user/registration-availability"}, {"POST", "/api/admin/email/register-code"},
	} {
		_, res = request(route.method, route.path, "", map[string]any{})
		if res["code"] != float64(101) {
			t.Fatal("disabled registration endpoint available")
		}
	}
}
