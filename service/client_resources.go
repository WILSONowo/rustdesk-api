package service

import (
	"errors"
	"net/url"
	"strings"
	"time"
	"unicode"

	"github.com/lejianwen/rustdesk-api/v2/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var ErrClientResourcesInvalid = errors.New("ClientResourcesInvalid")

func LoadClientResources() (*model.ClientResources, error) {
	value := &model.ClientResources{}
	err := DB.First(value, 1).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		value.ID = 1
		value.Downloads = []model.ClientDownload{}
		for _, platform := range []string{"Windows", "macOS", "Linux", "Android"} {
			value.Downloads = append(value.Downloads, model.ClientDownload{Platform: platform, Enabled: true})
		}
		return value, nil
	}
	return value, err
}

func ValidClientDownloadURL(raw string) bool {
	if raw == "" {
		return true
	}
	if len(raw) > 2048 || strings.Contains(raw, "\\") || strings.IndexFunc(raw, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }) >= 0 {
		return false
	}
	u, err := url.Parse(raw)
	if err != nil || u.User != nil || u.Opaque != "" {
		return false
	}
	if strings.HasPrefix(raw, "/") {
		// A root-relative path may later point at files hosted by this web server.
		decoded, err := url.PathUnescape(u.Path)
		return err == nil && u.Host == "" && !strings.HasPrefix(raw, "//") && !strings.HasPrefix(decoded, "//") && !strings.Contains(decoded, "\\")
	}
	return (u.Scheme == "https" || u.Scheme == "http") && u.Hostname() != ""
}

func SaveClientResources(downloads []model.ClientDownload, code string) (*model.ClientResources, error) {
	code = strings.TrimSpace(code)
	if len(downloads) > 24 || len(code) > 16384 || strings.IndexFunc(code, func(r rune) bool { return unicode.IsControl(r) && r != '\n' && r != '\r' && r != '\t' }) >= 0 {
		return nil, ErrClientResourcesInvalid
	}
	if downloads == nil {
		downloads = []model.ClientDownload{}
	}
	for i := range downloads {
		d := &downloads[i]
		d.Platform = strings.TrimSpace(d.Platform)
		d.Arch = strings.TrimSpace(d.Arch)
		d.Version = strings.TrimSpace(d.Version)
		d.URL = strings.TrimSpace(d.URL)
		if d.Platform != "Windows" && d.Platform != "macOS" && d.Platform != "Linux" && d.Platform != "Android" {
			return nil, ErrClientResourcesInvalid
		}
		if len(d.Arch) > 64 || len(d.Version) > 64 || !ValidClientDownloadURL(d.URL) {
			return nil, ErrClientResourcesInvalid
		}
	}
	value := &model.ClientResources{ID: 1, Downloads: downloads, ImportCode: code, UpdatedAt: time.Now()}
	err := DB.Clauses(clause.OnConflict{UpdateAll: true}).Create(value).Error
	return value, err
}
