package main

import (
	"io"
	"path/filepath"
	"testing"

	"github.com/lejianwen/rustdesk-api/v2/config"
	"github.com/lejianwen/rustdesk-api/v2/global"
	"github.com/lejianwen/rustdesk-api/v2/model"
	"github.com/sirupsen/logrus"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestClientResourcesUpgradeExistingInstallation(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "upgrade.db")), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, _ := db.DB()
	t.Cleanup(func() { sqlDB.Close() })
	if err := db.AutoMigrate(&model.Version{}, &model.User{}); err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.Version{Version: 266}).Error; err != nil {
		t.Fatal(err)
	}
	user := model.User{Username: "existing", Password: "unchanged-password-hash"}
	if err := db.Create(&user).Error; err != nil {
		t.Fatal(err)
	}
	global.DB, global.Config, global.Logger = db, config.Config{}, logrus.New()
	global.Logger.SetOutput(io.Discard)
	DatabaseAutoUpdate()
	if !db.Migrator().HasTable(&model.ClientResources{}) {
		t.Fatal("upgrade did not create resources table")
	}
	var after model.User
	if err := db.First(&after, user.Id).Error; err != nil || after.Password != user.Password {
		t.Fatal("upgrade changed existing user")
	}
	value := model.ClientResources{ID: 1, ImportCode: "saved-config", Downloads: []model.ClientDownload{}}
	if err := db.Create(&value).Error; err != nil {
		t.Fatal(err)
	}
	DatabaseAutoUpdate()
	var saved model.ClientResources
	if err := db.First(&saved, 1).Error; err != nil || saved.ImportCode != value.ImportCode {
		t.Fatal("restart lost settings")
	}
	var count int64
	db.Model(&model.Version{}).Count(&count)
	if count != 2 {
		t.Fatalf("unexpected repeated migration: %d", count)
	}
}
