package admin

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/lejianwen/rustdesk-api/v2/http/response"
	"github.com/lejianwen/rustdesk-api/v2/model"
	"github.com/lejianwen/rustdesk-api/v2/service"
)

type ClientResources struct{}

func (*ClientResources) Get(c *gin.Context) {
	c.Header("Cache-Control", "private, no-store")
	value, err := service.LoadClientResources()
	if err != nil {
		response.Fail(c, 101, response.TranslateMsg(c, "ClientResourcesLoadFailed"))
		return
	}
	if !service.AllService.UserService.IsAdmin(service.AllService.UserService.CurUser(c)) {
		enabled := make([]model.ClientDownload, 0, len(value.Downloads))
		for _, d := range value.Downloads {
			if d.Enabled {
				enabled = append(enabled, d)
			}
		}
		value.Downloads = enabled
	}
	response.Success(c, value)
}

func (*ClientResources) Save(c *gin.Context) {
	c.Header("Cache-Control", "private, no-store")
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 128<<10)
	var form struct {
		Downloads  []model.ClientDownload `json:"downloads"`
		ImportCode string                 `json:"import_code"`
	}
	if err := c.ShouldBindJSON(&form); err != nil {
		response.Fail(c, 101, response.TranslateMsg(c, "ClientResourcesInvalid"))
		return
	}
	value, err := service.SaveClientResources(form.Downloads, form.ImportCode)
	if errors.Is(err, service.ErrClientResourcesInvalid) {
		response.Fail(c, 101, response.TranslateMsg(c, "ClientResourcesInvalid"))
		return
	}
	if err != nil {
		response.Fail(c, 101, response.TranslateMsg(c, "ClientResourcesSaveFailed"))
		return
	}
	response.Success(c, value)
}
