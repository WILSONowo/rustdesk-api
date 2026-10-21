package admin

import (
	"context"
	"encoding/base64"
	"errors"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/lejianwen/rustdesk-api/v2/global"
	"github.com/lejianwen/rustdesk-api/v2/http/response"
	"github.com/lejianwen/rustdesk-api/v2/model"
	"github.com/lejianwen/rustdesk-api/v2/service"
	"gorm.io/gorm"
)

type ConnectionReadiness struct{}

// This reports observable account-service readiness, never endpoint reachability.
func (*ConnectionReadiness) Get(c *gin.Context) {
	c.Header("Cache-Control", "private, no-store")
	ctx, cancel := context.WithTimeout(c.Request.Context(), 2*time.Second)
	defer cancel()
	db, databaseErr := service.DB.DB()
	databaseOK := databaseErr == nil && db.PingContext(ctx) == nil
	resources := &model.ClientResources{}
	resourcesErr := service.DB.WithContext(ctx).First(resources, 1).Error
	resourcesLoaded := resourcesErr == nil || errors.Is(resourcesErr, gorm.ErrRecordNotFound)
	downloads, importCode := false, false
	if resourcesErr == nil {
		for _, item := range resources.Downloads {
			downloads = downloads || (item.Enabled && item.URL != "")
		}
		importCode = strings.TrimSpace(resources.ImportCode) != ""
	}
	key, keyErr := base64.StdEncoding.DecodeString(strings.TrimSpace(global.Config.Rustdesk.Key))
	response.Success(c, gin.H{
		"diagnostic_id": uuid.NewString(), "checked_at": time.Now().UTC().Format(time.RFC3339),
		"account_service": true, "database": databaseOK, "resources_loaded": resourcesLoaded,
		"downloads_configured": downloads, "import_code_configured": importCode,
		"id_server_configured": strings.TrimSpace(global.Config.Rustdesk.IdServer) != "",
		"public_key_valid":     keyErr == nil && len(key) == 32,
		"device_telemetry":     !global.Config.Gin.AccountsOrigin,
		"endpoint_connection":  "not_tested",
	})
}
