package device

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/iniwex5/vohive/internal/db"
)

func TestVoWiFiDeliveryStoreReportsMatchedPart(t *testing.T) {
	previousDB := db.DB
	if err := db.Init(filepath.Join(t.TempDir(), "delivery.db")); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if sqlDB, err := db.DB.DB(); err == nil {
			_ = sqlDB.Close()
		}
		db.DB = previousDB
	})

	store := vowifiDeliveryStore{}
	now := time.Now()
	if err := store.CreateSMSDelivery("message-1", "imsi-1", "wwan0", "+10086", "hello", 1, now); err != nil {
		t.Fatal(err)
	}
	if err := store.UpsertSMSDeliveryPart("message-1", 1, "call-1", 17, "pending", now); err != nil {
		t.Fatal(err)
	}
	match, err := store.MarkSMSDeliveryPartReport("call-1", "report-1", "wwan0", 17, "acked", 200, 0, "", now)
	if err != nil {
		t.Fatal(err)
	}
	if !match.Matched || match.MessageID != "message-1" || match.PartNo != 1 || match.State != "acked" {
		t.Fatalf("delivery match = %+v", match)
	}
}
