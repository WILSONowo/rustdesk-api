package admin

import (
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/lejianwen/rustdesk-api/v2/global"
	"github.com/lejianwen/rustdesk-api/v2/http/response"
	"github.com/lejianwen/rustdesk-api/v2/model"
	"github.com/lejianwen/rustdesk-api/v2/service"
)

type DeviceOverview struct{}
type deviceSummary struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Platform   string `json:"platform"`
	Owner      string `json:"owner"`
	Collection string `json:"collection"`
	LastSeen   int64  `json:"last_seen"`
}

func (*DeviceOverview) List(c *gin.Context) {
	c.Header("Cache-Control", "private, no-store")
	var query struct {
		Scope  string `form:"scope"`
		Search string `form:"search"`
		Page   int    `form:"page"`
	}
	if c.ShouldBindQuery(&query) != nil || (query.Scope != "" && query.Scope != "mine" && query.Scope != "shared") || query.Page < 0 || query.Page > 100000 || len(query.Search) > 100 {
		c.JSON(http.StatusBadRequest, response.Response{Code: 101, Message: "Invalid device query"})
		return
	}
	if query.Page == 0 {
		query.Page = 1
	}
	user := service.AllService.UserService.CurUser(c)
	rows := make([]deviceSummary, 0)
	var total int64
	search := "%" + strings.TrimSpace(query.Search) + "%"
	var err error
	if query.Scope != "shared" {
		tx := service.DB.Model(&model.Peer{}).Where("user_id = ?", user.Id)
		if query.Search != "" {
			tx = tx.Where("id LIKE ? OR hostname LIKE ? OR alias LIKE ?", search, search, search)
		}
		var peers []model.Peer
		err = tx.Count(&total).Error
		if err == nil {
			err = tx.Order("last_online_time DESC, row_id DESC").Offset((query.Page - 1) * 20).Limit(20).Find(&peers).Error
		}
		for _, peer := range peers {
			name := peer.Alias
			if name == "" {
				name = peer.Hostname
			}
			rows = append(rows, deviceSummary{ID: peer.Id, Name: name, Platform: peer.Os, Owner: user.Username, LastSeen: peer.LastOnlineTime})
		}
	} else {
		// Re-evaluate grants for every read, including expiry and disabled owners.
		grants := service.DB.Model(&model.AddressBookCollectionRule{}).Select("collection_id").
			Where("rule > 0 AND (expires_at = 0 OR expires_at > ?)", time.Now().Unix()).
			Where("(type = ? AND to_id = ?) OR (type = ? AND to_id = ?)", model.ShareAddressBookRuleTypePersonal, user.Id, model.ShareAddressBookRuleTypeGroup, user.GroupId).
			Where("user_id IN (?)", service.DB.Model(&model.User{}).Select("id").Where("status = ?", model.COMMON_STATUS_ENABLE))
		tx := service.DB.Model(&model.AddressBook{}).Where("collection_id IN (?) AND user_id <> ?", grants, user.Id)
		if query.Search != "" {
			tx = tx.Where("id LIKE ? OR hostname LIKE ? OR alias LIKE ?", search, search, search)
		}
		var books []model.AddressBook
		err = tx.Count(&total).Error
		if err == nil {
			err = tx.Select("row_id", "id", "alias", "hostname", "platform", "user_id", "collection_id").Preload("Collection").Order("row_id DESC").Offset((query.Page - 1) * 20).Limit(20).Find(&books).Error
		}
		for _, book := range books {
			name := book.Alias
			if name == "" {
				name = book.Hostname
			}
			collection := ""
			if book.Collection != nil {
				collection = book.Collection.Name
			}
			owner := service.AllService.UserService.InfoById(book.UserId)
			rows = append(rows, deviceSummary{ID: book.Id, Name: name, Platform: book.Platform, Owner: owner.Username, Collection: collection})
		}
	}
	if err != nil {
		response.Fail(c, 101, response.TranslateMsg(c, "OperationFailed"))
		return
	}
	response.Success(c, gin.H{"list": rows, "total": total, "page": query.Page, "page_size": 20, "device_telemetry": !global.Config.Gin.AccountsOrigin})
}
