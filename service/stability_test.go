package service

import (
	"io"
	"path/filepath"
	"testing"
	"time"

	"github.com/lejianwen/rustdesk-api/v2/config"
	"github.com/lejianwen/rustdesk-api/v2/lib/lock"
	"github.com/lejianwen/rustdesk-api/v2/model"
	"github.com/sirupsen/logrus"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func stabilityDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "test.sqlite")), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	if err = db.AutoMigrate(&model.User{}, &model.UserThird{}, &model.Oauth{}, &model.Peer{}, &model.AddressBookCollection{}, &model.AddressBookCollectionRule{}, &model.UserToken{}, &model.LoginLog{}); err != nil {
		t.Fatal(err)
	}
	sqlDB, _ := db.DB()
	t.Cleanup(func() { sqlDB.Close() })
	log := logrus.New()
	log.SetOutput(io.Discard)
	New(&config.Config{}, db, log, nil, lock.NewLocal())
	return db
}

func TestDeviceLoginCannotTransferOwnership(t *testing.T) {
	db := stabilityDB(t)
	peer := model.Peer{Id: "device-1", Uuid: "uuid-1", UserId: 42}
	if err := db.Create(&peer).Error; err != nil {
		t.Fatal(err)
	}
	AllService.PeerService.UuidBindUserId(peer.Id, peer.Uuid, 99)
	if owner := AllService.PeerService.InfoByRowId(peer.RowId).UserId; owner != 42 {
		t.Fatalf("ownership changed to %d", owner)
	}
	db.Model(&peer).Update("user_id", 0)
	AllService.PeerService.UuidBindUserId("wrong-id", peer.Uuid, 99)
	if owner := AllService.PeerService.InfoByRowId(peer.RowId).UserId; owner != 0 {
		t.Fatal("UUID alone claimed device")
	}
	AllService.PeerService.UuidBindUserId(peer.Id, peer.Uuid, 42)
	if owner := AllService.PeerService.InfoByRowId(peer.RowId).UserId; owner != 42 {
		t.Fatal("initial binding failed")
	}
}

func TestOAuthEmailVerificationSurvivesCacheAndControlsLinking(t *testing.T) {
	for _, verified := range []bool{false, true} {
		t.Run(map[bool]string{false: "unverified", true: "verified"}[verified], func(t *testing.T) {
			db := stabilityDB(t)
			owner := &model.User{Username: "owner", Email: "owner@example.test", EmailVerified: true, Status: model.COMMON_STATUS_ENABLE}
			if err := db.Create(owner).Error; err != nil {
				t.Fatal(err)
			}
			if err := db.Create(&model.Oauth{Op: "test", OauthType: model.OauthTypeOidc}).Error; err != nil {
				t.Fatal(err)
			}
			remote := &model.OauthUser{OpenId: "subject", Username: "remote", Email: owner.Email, VerifiedEmail: verified}
			cache := &OauthCacheItem{}
			cache.UpdateFromOauthUser(remote)
			if cache.ToOauthUser().VerifiedEmail != verified {
				t.Fatal("cache dropped verification")
			}
			err, result := AllService.UserService.RegisterByOauth(cache.ToOauthUser(), "test")
			if err != nil {
				t.Fatal(err)
			}
			if (result.Id == owner.Id) != verified {
				t.Fatalf("wrong account linked: %d", result.Id)
			}
			if !verified && result.Status != model.USER_STATUS_PENDING {
				t.Fatal("unverified registration bypassed approval")
			}
		})
	}
}

func TestOAuthCannotLinkUnverifiedLocalAccount(t *testing.T) {
	db := stabilityDB(t)
	user := &model.User{Username: "local", Email: "local@example.test"}
	db.Create(user)
	db.Create(&model.Oauth{Op: "test", OauthType: model.OauthTypeOidc})
	err, _ := AllService.UserService.RegisterByOauth(&model.OauthUser{OpenId: "subject", Username: "remote", Email: user.Email, VerifiedEmail: true}, "test")
	if err == nil {
		t.Fatal("unverified local email linked")
	}
	var count int64
	db.Model(&model.UserThird{}).Count(&count)
	if count != 0 {
		t.Fatal("binding persisted on rejection")
	}
}

func TestSharingExpiryRevocationAndOwnerDisable(t *testing.T) {
	db := stabilityDB(t)
	owner := &model.User{Username: "owner", Status: model.COMMON_STATUS_ENABLE}
	recipient := &model.User{Username: "recipient", GroupId: 4, Status: model.COMMON_STATUS_ENABLE}
	db.Create(owner)
	db.Create(recipient)
	collection := &model.AddressBookCollection{UserId: owner.Id, Name: "Shared"}
	db.Create(collection)
	rule := &model.AddressBookCollectionRule{UserId: owner.Id, CollectionId: collection.Id, Rule: 3, Type: 1, ToId: recipient.Id, ExpiresAt: time.Now().Unix() + 3600}
	if err := AllService.AddressBookService.CreateRule(rule); err != nil {
		t.Fatal(err)
	}
	check := func(want int) {
		t.Helper()
		if got := AllService.AddressBookService.UserMaxRule(recipient, owner.Id, collection.Id); got != want {
			t.Fatalf("permission %d, expected %d", got, want)
		}
		if got := len(AllService.AddressBookService.CollectionReadRules(recipient)); (got > 0) != (want > 0) {
			t.Fatal("list and permission disagree")
		}
	}
	check(3)
	rule.ExpiresAt = time.Now().Unix() - 1
	AllService.AddressBookService.UpdateRule(rule)
	check(0)
	rule.ExpiresAt = 0
	AllService.AddressBookService.UpdateRule(rule)
	check(3)
	db.Model(owner).Update("status", 2)
	check(0)
	db.Model(owner).Update("status", model.COMMON_STATUS_ENABLE)
	check(3)
	AllService.AddressBookService.DeleteRule(rule)
	check(0)
	// Group grants are subject to the same expiry check.
	rule.Id = 0
	rule.Type = 2
	rule.ToId = recipient.GroupId
	rule.ExpiresAt = time.Now().Unix() - 1
	AllService.AddressBookService.CreateRule(rule)
	check(0)
}
