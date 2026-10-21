package router

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/lejianwen/rustdesk-api/v2/config"
	"github.com/lejianwen/rustdesk-api/v2/global"
	apiController "github.com/lejianwen/rustdesk-api/v2/http/controller/api"
	"github.com/lejianwen/rustdesk-api/v2/http/middleware"
	"github.com/lejianwen/rustdesk-api/v2/lib/lock"
	"github.com/lejianwen/rustdesk-api/v2/model"
	"github.com/lejianwen/rustdesk-api/v2/service"
	"github.com/lejianwen/rustdesk-api/v2/utils"
	"github.com/sirupsen/logrus"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func stabilityRouter(t *testing.T) (*gin.Engine, *gorm.DB) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "http.sqlite")), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	err = db.AutoMigrate(&model.User{}, &model.UserToken{}, &model.LoginLog{}, &model.Oauth{}, &model.Group{}, &model.Peer{}, &model.Tag{}, &model.AddressBook{}, &model.AddressBookCollection{}, &model.AddressBookCollectionRule{}, &model.ClientResources{})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, _ := db.DB()
	t.Cleanup(func() { sqlDB.Close() })
	global.Config = config.Config{Gin: config.Gin{ResourcesPath: "../../resources", AccountsOrigin: true}, App: config.App{CaptchaThreshold: 3, BanThreshold: 5}}
	global.Logger = logrus.New()
	global.Logger.SetOutput(io.Discard)
	global.InitI18n()
	global.ApiInitValidator()
	service.New(&global.Config, db, global.Logger, nil, lock.NewLocal())
	global.LoginLimiter = utils.NewLoginLimiter(utils.SecurityPolicy{CaptchaThreshold: 3, BanThreshold: 5, AttemptsWindow: time.Minute})
	hash, err := utils.EncryptPassword("test-pass-1")
	if err != nil {
		t.Fatal(err)
	}
	for _, u := range []model.User{{IdModel: model.IdModel{Id: 1}, Username: "owner", Password: hash, Email: "private@example.test", Remark: "private-note", Status: 1, GroupId: 1}, {IdModel: model.IdModel{Id: 2}, Username: "member", Password: hash, Status: 1, GroupId: 1}, {IdModel: model.IdModel{Id: 3}, Username: "pending", Password: hash, Status: 3}} {
		if err := db.Create(&u).Error; err != nil {
			t.Fatal(err)
		}
		if err := db.Create(&model.UserToken{UserId: u.Id, Token: u.Username, ExpiredAt: time.Now().Unix() + 864000}).Error; err != nil {
			t.Fatal(err)
		}
	}
	g := gin.New()
	g.SetTrustedProxies(nil)
	g.Use(middleware.AccountsOrigin(), middleware.Limiter())
	Init(g)
	g.POST("/api/login", (&apiController.Login{}).Login)
	g.POST("/api/heartbeat", func(c *gin.Context) { c.JSON(200, gin.H{"unexpected": true}) })
	return g, db
}

func request(t *testing.T, g *gin.Engine, method, path, token, body string) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	r.RemoteAddr = "192.0.2.1:12345"
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("api-token", token)
	w := httptest.NewRecorder()
	g.ServeHTTP(w, r)
	return w
}
func responseData(t *testing.T, w *httptest.ResponseRecorder) map[string]interface{} {
	t.Helper()
	var body struct {
		Code int                    `json:"code"`
		Data map[string]interface{} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if w.Code != 200 || body.Code != 0 {
		t.Fatalf("request failed: %d %s", w.Code, w.Body.String())
	}
	return body.Data
}

func TestDeviceOverviewAndSharingEnforceAccess(t *testing.T) {
	g, db := stabilityRouter(t)
	db.Create(&model.Peer{Id: "mine", Hostname: "My PC", UserId: 2})
	db.Create(&model.Peer{Id: "hidden", Hostname: "Secret PC", UserId: 1})
	collection := &model.AddressBookCollection{UserId: 1, Name: "Team"}
	db.Create(collection)
	db.Create(&model.AddressBook{Id: "shared", Hostname: "Shared PC", UserId: 1, CollectionId: collection.Id, Password: "never-export", Hash: "never-export"})
	rule := &model.AddressBookCollectionRule{UserId: 1, CollectionId: collection.Id, Type: 1, ToId: 2, Rule: 1, ExpiresAt: time.Now().Unix() + 100}
	db.Create(rule)
	data := responseData(t, request(t, g, "GET", "/api/admin/my/device-overview", "member", ""))
	if data["total"] != float64(1) || data["list"].([]interface{})[0].(map[string]interface{})["id"] != "mine" {
		t.Fatal(data)
	}
	w := request(t, g, "GET", "/api/admin/my/device-overview?scope=shared", "member", "")
	data = responseData(t, w)
	if data["total"] != float64(1) || strings.Contains(w.Body.String(), "never-export") {
		t.Fatal(w.Body.String())
	}
	db.Model(rule).Update("expires_at", time.Now().Unix()-1)
	data = responseData(t, request(t, g, "GET", "/api/admin/my/device-overview?scope=shared", "member", ""))
	if data["total"] != float64(0) {
		t.Fatal("expired share visible")
	}
	if request(t, g, "GET", "/api/admin/my/device-overview?page=-1", "member", "").Code != 400 {
		t.Fatal("negative page accepted")
	}
	data = responseData(t, request(t, g, "POST", "/api/admin/user/groupUsers", "member", "{}"))
	encoded, _ := json.Marshal(data["users"])
	if strings.Contains(string(encoded), "private") || strings.Contains(string(encoded), "email") || strings.Contains(string(encoded), "pending") {
		t.Fatal(string(encoded))
	}
	// A member cannot attach a private tag to another owner's collection.
	body := `{"name":"bad","collection_id":1}`
	w = request(t, g, "POST", "/api/admin/my/tag/create", "member", body)
	var envelope map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &envelope)
	if envelope["code"] == float64(0) {
		t.Fatal("foreign collection accepted")
	}
	// Previously malformed rows must not disclose collection names either.
	db.Create(&model.Tag{Name: "legacy", UserId: 2, CollectionId: collection.Id})
	w = request(t, g, "GET", "/api/admin/my/tag/list", "member", "")
	if strings.Contains(w.Body.String(), "Team") {
		t.Fatal("foreign collection name leaked")
	}
}

func TestReadinessIsAuthenticatedAndPreservesAccountsOnlyBoundary(t *testing.T) {
	g, _ := stabilityRouter(t)
	w := request(t, g, "GET", "/api/admin/connection-readiness", "", "")
	if strings.Contains(w.Body.String(), "diagnostic_id") {
		t.Fatal("anonymous readiness exposed")
	}
	data := responseData(t, request(t, g, "GET", "/api/admin/connection-readiness", "member", ""))
	if data["database"] != true || data["device_telemetry"] != false || data["endpoint_connection"] != "not_tested" {
		t.Fatal(data)
	}
	for _, key := range []string{"password", "token", "import_code", "key", "id_server"} {
		if _, ok := data[key]; ok {
			t.Fatalf("sensitive field %s", key)
		}
	}
	if request(t, g, "POST", "/api/heartbeat", "", "{}").Code != 404 {
		t.Fatal("account-only boundary removed")
	}
	if request(t, g, "GET", "/api/admin/connection-readiness", "pending", "").Code != 401 {
		t.Fatal("pending account accepted")
	}
}

func TestPasswordFailuresCannotBeResetBySuccessfulLogin(t *testing.T) {
	g, _ := stabilityRouter(t)
	bad := `{"username":"owner","password":"bad-password"}`
	good := `{"username":"member","password":"test-pass-1"}`
	if request(t, g, "POST", "/api/login", "", bad).Code != 400 {
		t.Fatal("bad login accepted")
	}
	if request(t, g, "POST", "/api/admin/login", "", good).Code != 200 {
		t.Fatal("good web login rejected")
	}
	request(t, g, "POST", "/api/login", "", bad)
	request(t, g, "POST", "/api/login", "", bad)
	if request(t, g, "POST", "/api/login", "", good).Code != http.StatusTooManyRequests {
		t.Fatal("success cleared shared failure history or native bypassed threshold")
	}
	// Already authorized sessions behind the same NAT must keep working after a ban.
	for i := 0; i < 5; i++ {
		global.LoginLimiter.RecordFailedAttempt("192.0.2.1")
	}
	responseData(t, request(t, g, "GET", "/api/admin/connection-readiness", "member", ""))
	if request(t, g, "POST", "/api/admin/login", "", good).Code != 429 {
		t.Fatal("web ban not enforced")
	}
	// Untrusted X-Forwarded-For cannot evade the same limit.
	r := httptest.NewRequest("POST", "/api/login", strings.NewReader(good))
	r.RemoteAddr = "192.0.2.1:23"
	r.Header.Set("X-Forwarded-For", "198.51.100.8")
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	g.ServeHTTP(w, r)
	if w.Code != 429 {
		t.Fatal("spoofed client IP bypassed ban")
	}
}
