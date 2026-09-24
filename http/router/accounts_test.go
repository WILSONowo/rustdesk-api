package router

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/lejianwen/rustdesk-api/v2/config"
	"github.com/lejianwen/rustdesk-api/v2/global"
	"github.com/lejianwen/rustdesk-api/v2/lib/jwt"
	"github.com/lejianwen/rustdesk-api/v2/lib/lock"
	"github.com/lejianwen/rustdesk-api/v2/model"
	"github.com/lejianwen/rustdesk-api/v2/service"
	"github.com/lejianwen/rustdesk-api/v2/utils"
	"github.com/sirupsen/logrus"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestAccountApprovalAndAddressBooks(t *testing.T) {
	previousDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir("../.."); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(previousDir) })
	gin.SetMode(gin.TestMode)
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "accounts.db")), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, _ := db.DB()
	t.Cleanup(func() { sqlDB.Close() })
	if err := db.AutoMigrate(&model.ClientResources{}, &model.User{}, &model.EmailIdentity{}, &model.EmailChallenge{}, &model.MailMessage{}, &model.MailRate{}, &model.UserToken{}, &model.LoginLog{}, &model.Group{}, &model.Oauth{}, &model.Peer{}, &model.Tag{}, &model.AddressBook{}, &model.AddressBookCollection{}, &model.AddressBookCollectionRule{}); err != nil {
		t.Fatal(err)
	}
	global.Config = config.Config{Lang: "en"}
	global.Config.App.Register = true
	global.Config.App.RegisterStatus = 1 // Legacy config must not bypass approval.
	global.Config.App.TokenExpire = time.Hour
	global.Config.Gin.ResourcesPath = "resources"
	global.Logger = logrus.New()
	global.Logger.SetOutput(io.Discard)
	global.Jwt = jwt.NewJwt("", time.Hour)
	global.LoginLimiter = utils.NewLoginLimiter(utils.SecurityPolicy{CaptchaThreshold: -1})
	global.InitI18n()
	global.ApiInitValidator()
	service.New(&global.Config, db, global.Logger, global.Jwt, lock.NewLocal())
	service.AllService.RegistrationService = service.NewRegistrationService(db, registrationTestCaptcha{})
	r := gin.New()
	Init(r)
	ApiInit(r)

	request := func(method, path, token string, body any) (int, map[string]any) {
		t.Helper()
		b, _ := json.Marshal(body)
		req := httptest.NewRequest(method, path, bytes.NewReader(b))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("accept-lang", "en")
		if token != "" {
			req.Header.Set("api-token", token)
			req.Header.Set("Authorization", "Bearer "+token)
		}
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		var result map[string]any
		if w.Body.Len() == 0 {
			return w.Code, result
		}
		if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
			t.Fatalf("%s %s returned invalid JSON: %s", method, path, w.Body.String())
		}
		return w.Code, result
	}
	expectCode := func(result map[string]any, code float64) {
		t.Helper()
		if result["code"] != code {
			t.Fatalf("expected code %v, got %#v", code, result)
		}
	}
	createUser := func(name string, admin bool) (*model.User, string) {
		t.Helper()
		u := &model.User{Username: name, Password: "test-password-123", GroupId: 1, IsAdmin: &admin, Status: model.COMMON_STATUS_ENABLE}
		if err := service.AllService.UserService.Create(u); err != nil {
			t.Fatal(err)
		}
		ut := service.AllService.UserService.Login(u, &model.LoginLog{})
		if ut == nil {
			t.Fatal("login did not create a session")
		}
		return u, ut.Token
	}
	addCaptcha := func(form map[string]any) {
		t.Helper()
		_, res := request("GET", "/api/admin/user/registration-captcha", "", nil)
		expectCode(res, 0)
		form["captcha_id"] = res["data"].(map[string]any)["captcha"].(map[string]any)["id"]
		form["captcha"] = "abcd"
	}
	admin, adminToken := createUser("admin", true)
	other, otherToken := createUser("other", false)
	t.Run("client resources permissions and persistence", func(t *testing.T) { testClientResources(t, request, adminToken, otherToken) })
	form := map[string]any{"username": "alice", "email": "alice@example.test", "password": "test-password-123", "confirm_password": "test-password-123", "status": 1, "is_admin": true}
	_, missingCaptcha := request("POST", "/api/admin/user/register", "", form)
	expectCode(missingCaptcha, 101)
	addCaptcha(form)
	_, res := request("POST", "/api/admin/user/register", "", form)
	expectCode(res, 0)
	if res["data"].(map[string]any)["pending_approval"] != true {
		t.Fatal("registration did not indicate approval")
	}
	alice := service.AllService.UserService.InfoByUsername("alice")
	if alice.Status != model.USER_STATUS_PENDING || service.AllService.UserService.IsAdmin(alice) {
		t.Fatalf("unsafe signup state: %+v", alice)
	}
	for field, value := range map[string]string{"username": "ALICE", "email": "ALICE@example.test"} {
		_, check := request("POST", "/api/admin/user/registration-availability", "", map[string]any{"field": field, "value": value})
		expectCode(check, 0)
		if check["data"].(map[string]any)["available"] != false {
			t.Fatal("pending identity was available")
		}
	}
	duplicate := map[string]any{"username": "another", "email": "ALICE@example.test", "password": "test-password-123", "confirm_password": "test-password-123"}
	addCaptcha(duplicate)
	_, duplicateResult := request("POST", "/api/admin/user/register", "", duplicate)
	expectCode(duplicateResult, 101)
	var tokenCount int64
	db.Model(&model.UserToken{}).Where("user_id = ?", alice.Id).Count(&tokenCount)
	if tokenCount != 0 {
		t.Fatal("signup created an active session")
	}
	loginBody := map[string]any{"username": "alice", "password": "test-password-123"}
	loginStatus, res := request("POST", "/api/admin/login", "", loginBody)
	if loginStatus != 401 {
		t.Fatalf("pending login returned HTTP %d", loginStatus)
	}
	expectCode(res, 101)
	if !strings.Contains(res["message"].(string), "approval") {
		t.Fatalf("missing approval explanation: %v", res)
	}
	loginStatus, res = request("POST", "/api/admin/login", "", map[string]any{"username": "alice", "password": "wrong-password-123"})
	if loginStatus != 401 || res["data"] != nil {
		t.Fatal("failed login must return 401 without credentials")
	}
	expectCode(res, 101)
	status, res := request("POST", "/api/login", "", loginBody)
	if status != 400 || res["access_token"] != nil {
		t.Fatal("client logged in before approval")
	}
	if service.AllService.UserService.Login(alice, &model.LoginLog{}) != nil {
		t.Fatal("central login bypassed approval")
	}

	_, res = request("GET", "/api/admin/user/list?status=3", adminToken, nil)
	expectCode(res, 0)
	if res["data"].(map[string]any)["total"] != float64(1) {
		t.Fatal("pending filter failed")
	}
	_, res = request("POST", "/api/admin/user/review", "", map[string]any{"id": alice.Id, "approve": true})
	expectCode(res, 403)
	_, res = request("POST", "/api/admin/user/review", otherToken, map[string]any{"id": alice.Id, "approve": true})
	expectCode(res, 403)
	for _, route := range []struct{ method, path string }{{"GET", "capabilities"}, {"POST", "sendCmd"}, {"GET", "cmdList"}, {"POST", "cmdDelete"}, {"POST", "cmdCreate"}} {
		_, res = request(route.method, "/api/admin/rustdesk/"+route.path, otherToken, map[string]any{})
		expectCode(res, 403)
	}
	_, res = request("GET", "/api/admin/rustdesk/capabilities", adminToken, nil)
	expectCode(res, 0)
	if res["data"].(map[string]any)["enabled"] != false {
		t.Fatal("commands should be disabled by default")
	}
	_, res = request("POST", "/api/admin/rustdesk/sendCmd", adminToken, map[string]any{"cmd": "h", "target": model.ServerCmdTargetIdServer})
	expectCode(res, 101)
	if res["message"] != "Server commands are not enabled." {
		t.Fatalf("unexpected disabled response: %#v", res)
	}
	global.Config.Admin.ServerCommandsEnabled = true
	_, res = request("GET", "/api/admin/rustdesk/capabilities", adminToken, nil)
	expectCode(res, 0)
	if res["data"].(map[string]any)["enabled"] != true {
		t.Fatal("enabled commands capability missing")
	}
	global.Config.Admin.ServerCommandsEnabled = false
	_, res = request("POST", "/api/admin/user/review", adminToken, map[string]any{"id": alice.Id})
	expectCode(res, 101)
	_, res = request("POST", "/api/admin/user/review", adminToken, map[string]any{"id": alice.Id, "approve": true})
	expectCode(res, 0)
	_, res = request("POST", "/api/admin/user/review", adminToken, map[string]any{"id": alice.Id, "approve": false})
	expectCode(res, 101)
	status, res = request("POST", "/api/login", "", loginBody)
	if status != 200 {
		t.Fatalf("approved client login failed: %v", res)
	}
	aliceToken, ok := res["access_token"].(string)
	if !ok {
		t.Fatalf("client login response: %v", res)
	}
	randomBytes, err := base64.RawURLEncoding.DecodeString(aliceToken)
	if err != nil || len(randomBytes) != 32 {
		t.Fatal("expected a 256-bit opaque token")
	}

	personal := fmt.Sprintf("1-%d-0", alice.Id)
	status, res = request("POST", "/api/ab/peer/add/"+personal, aliceToken, map[string]any{"id": "123456789", "username": "pc-user", "hostname": "personal", "platform": "Windows", "tags": []string{}})
	if status != 200 {
		t.Fatalf("personal add failed: %v", res)
	}
	status, res = request("POST", "/api/ab/peers?ab="+personal, aliceToken, nil)
	if status != 200 || res["total"] != float64(1) {
		t.Fatalf("personal read failed: %v", res)
	}
	status, _ = request("POST", "/api/ab/peers?ab="+personal, otherToken, nil)
	if status != 400 {
		t.Fatal("personal address book exposed to another user")
	}
	collection := &model.AddressBookCollection{UserId: alice.Id, Name: "Shared"}
	if err := db.Create(collection).Error; err != nil {
		t.Fatal(err)
	}
	rule := &model.AddressBookCollectionRule{UserId: alice.Id, CollectionId: collection.Id, Type: model.ShareAddressBookRuleTypePersonal, ToId: other.Id, Rule: model.ShareAddressBookRuleRuleRead}
	if err := db.Create(rule).Error; err != nil {
		t.Fatal(err)
	}
	shared := fmt.Sprintf("1-%d-%d", alice.Id, collection.Id)
	status, _ = request("POST", "/api/ab/peers?ab="+shared, otherToken, nil)
	if status != 200 {
		t.Fatal("shared read was denied")
	}
	status, _ = request("POST", "/api/ab/peer/add/"+shared, otherToken, map[string]any{"id": "987654321"})
	if status != 400 {
		t.Fatal("read-only recipient could write")
	}
	if err := db.Delete(rule).Error; err != nil {
		t.Fatal(err)
	}
	status, _ = request("POST", "/api/ab/peers?ab="+shared, otherToken, nil)
	if status != 400 {
		t.Fatal("revoked sharing still accessible")
	}

	_, res = request("POST", "/api/admin/logout", aliceToken, nil)
	expectCode(res, 0)
	status, _ = request("POST", "/api/ab/peers?ab="+personal, aliceToken, nil)
	if status != 401 {
		t.Fatal("logout did not revoke token")
	}

	form["username"] = "rejected"
	form["email"] = "rejected@example.test"
	addCaptcha(form)
	_, res = request("POST", "/api/admin/user/register", "", form)
	expectCode(res, 0)
	rejected := service.AllService.UserService.InfoByUsername("rejected")
	_, res = request("POST", "/api/admin/user/review", adminToken, map[string]any{"id": rejected.Id, "approve": false})
	expectCode(res, 0)
	if service.AllService.UserService.InfoById(rejected.Id).Status != model.COMMON_STATUS_DISABLED {
		t.Fatal("rejection not persisted")
	}
	if service.AllService.UserService.Login(rejected, &model.LoginLog{}) != nil {
		t.Fatal("rejected account could log in")
	}
	form["username"], form["confirm_password"] = "mismatch", "different-password"
	_, res = request("POST", "/api/admin/user/register", "", form)
	expectCode(res, 101)
	if service.AllService.UserService.InfoByUsername("mismatch").Id != 0 {
		t.Fatal("mismatched passwords registered")
	}
	global.Config.App.Register = false
	_, res = request("POST", "/api/admin/user/register", "", form)
	expectCode(res, 101)

	// Disabling revokes existing sessions; re-enabling must not resurrect them.
	other.Status = model.COMMON_STATUS_DISABLED
	if err := service.AllService.UserService.Update(other); err != nil {
		t.Fatal(err)
	}
	other.Status = model.COMMON_STATUS_ENABLE
	if err := service.AllService.UserService.Update(other); err != nil {
		t.Fatal(err)
	}
	_, res = request("GET", "/api/admin/user/current", otherToken, nil)
	expectCode(res, 403)
	admin.Status = model.USER_STATUS_PENDING
	if service.AllService.UserService.Update(admin) == nil {
		t.Fatal("last active administrator could be disabled")
	}

	// The public registration route fails closed until mail is configured.
	global.Config.App.Register = true
	global.Config.Mail = config.Mail{RequireRegistrationVerification: true}
	service.AllService.MailService = service.NewMailService(db, global.Config.Mail)
	emailForm := map[string]any{"username": "mailflow", "email": "mailflow@example.test", "password": "mail-password-123", "confirm_password": "mail-password-123", "email_verified": true}
	_, res = request("POST", "/api/admin/user/register", "", emailForm)
	expectCode(res, 101)
	global.Config.Mail = config.Mail{Enabled: true, RequireRegistrationVerification: true, Host: "smtp.qq.com", Port: 465, From: "sender@example.test", TLS: "implicit"}
	service.AllService.MailService = service.NewMailService(db, global.Config.Mail)
	_, res = request("POST", "/api/admin/user/register", "", emailForm)
	expectCode(res, 101)
	_, res = request("POST", "/api/admin/email/bind-code", "", map[string]any{"email": "bind@example.test", "password": "mail-password-123"})
	expectCode(res, 403)
	codeForm := map[string]any{"email": "mailflow@example.test"}
	_, res = request("POST", "/api/admin/email/register-code", "", codeForm)
	expectCode(res, 101)
	addCaptcha(codeForm)
	_, res = request("POST", "/api/admin/email/register-code", "", codeForm)
	expectCode(res, 0)
	emailForm["challenge_id"] = res["data"].(map[string]any)["challenge_id"]
	var queued model.MailMessage
	db.First(&queued, "challenge_id = ?", emailForm["challenge_id"])
	codeMatch := regexp.MustCompile(`验证码：(\d{8})`).FindStringSubmatch(queued.Body)
	if len(codeMatch) != 2 {
		t.Fatal("registration email was not queued")
	}
	emailForm["code"] = codeMatch[1]
	_, res = request("POST", "/api/admin/user/register", "", emailForm)
	expectCode(res, 0)
	mailUser := service.AllService.UserService.InfoByUsername("mailflow")
	if !mailUser.EmailVerified || mailUser.Status != model.USER_STATUS_PENDING {
		t.Fatal("mail route registration did not verify and require approval")
	}
	_, res = request("POST", "/api/admin/user/review", adminToken, map[string]any{"id": mailUser.Id, "approve": true})
	expectCode(res, 0)
	db.Where("1 = 1").Delete(&model.MailRate{})
	_, res = request("POST", "/api/admin/email/reset-code", "", map[string]any{"email": mailUser.Email})
	expectCode(res, 0)
	resetID := res["data"].(map[string]any)["challenge_id"]
	queued = model.MailMessage{}
	db.First(&queued, "challenge_id = ?", resetID)
	codeMatch = regexp.MustCompile(`验证码：(\d{8})`).FindStringSubmatch(queued.Body)
	resetBody := map[string]any{"email": mailUser.Email, "challenge_id": resetID, "code": codeMatch[1], "new_password": "reset-password-123", "confirm_password": "mismatch"}
	_, res = request("POST", "/api/admin/email/reset-password", "", resetBody)
	expectCode(res, 101)
	resetBody["confirm_password"] = "reset-password-123"
	_, res = request("POST", "/api/admin/email/reset-password", "", resetBody)
	expectCode(res, 0)
	loginStatus, res = request("POST", "/api/admin/login", "", map[string]any{"username": "mailflow", "password": "reset-password-123"})
	if loginStatus != 200 {
		t.Fatalf("successful login returned HTTP %d", loginStatus)
	}
	expectCode(res, 0)

	// A storage failure must never produce a usable login response.
	t.Run("registration captcha and rate limits", func(t *testing.T) { testRegistrationGuards(t, request) })
	if err := db.Migrator().DropTable(&model.LoginLog{}); err != nil {
		t.Fatal(err)
	}
	before := int64(0)
	db.Model(&model.UserToken{}).Count(&before)
	if service.AllService.UserService.Login(other, &model.LoginLog{}) != nil {
		t.Fatal("login succeeded despite storage failure")
	}
	db.Model(&model.UserToken{}).Count(&tokenCount)
	if before != tokenCount {
		t.Fatal("failed login left an orphan session")
	}
}
