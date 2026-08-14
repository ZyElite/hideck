package db

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func TestDisclaimerAcceptanceStorePersistsAcrossInstances(t *testing.T) {
	database, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "consent.db")), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := database.AutoMigrate(&DisclaimerAcceptance{}); err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	acceptedAt := time.Date(2026, time.August, 14, 12, 30, 0, 0, time.FixedZone("CST", 8*60*60))
	first := NewDisclaimerAcceptanceStore(database)
	if _, accepted, err := first.Status(ctx, "1"); err != nil || accepted {
		t.Fatalf("initial status accepted=%v err=%v", accepted, err)
	}
	if _, err := first.Accept(ctx, "1", acceptedAt); err != nil {
		t.Fatal(err)
	}

	second := NewDisclaimerAcceptanceStore(database)
	storedAt, accepted, err := second.Status(ctx, "1")
	if err != nil {
		t.Fatal(err)
	}
	if !accepted || !storedAt.Equal(acceptedAt) {
		t.Fatalf("status accepted=%v acceptedAt=%v", accepted, storedAt)
	}
	if _, accepted, err := second.Status(ctx, "2"); err != nil || accepted {
		t.Fatalf("new version status accepted=%v err=%v", accepted, err)
	}
}

func TestDisclaimerAcceptanceStoreUpdatesSingleton(t *testing.T) {
	database, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "consent.db")), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := database.AutoMigrate(&DisclaimerAcceptance{}); err != nil {
		t.Fatal(err)
	}
	store := NewDisclaimerAcceptanceStore(database)
	ctx := context.Background()
	for _, version := range []string{"1", "2"} {
		if _, err := store.Accept(ctx, version, time.Now()); err != nil {
			t.Fatal(err)
		}
	}
	var count int64
	if err := database.Model(&DisclaimerAcceptance{}).Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("acceptance rows=%d want=1", count)
	}
}
