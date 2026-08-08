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
	if err := store.MarkSMSDeliveryPartSIPResult("message-1", 1, 202, "pending", "", now); err != nil {
		t.Fatal(err)
	}
	pending, err := store.GetSMSDeliveryStatus("message-1")
	if err != nil {
		t.Fatal(err)
	}
	if pending.State != "pending" || pending.Parts[0].SIPCode != 202 || pending.Parts[0].ReportAt != nil {
		t.Fatalf("pending SIP result = %+v", pending)
	}
	match, err := store.MarkSMSDeliveryPartReport("call-1", "report-1", "wwan0", 17, "acked", 200, 0, "", now)
	if err != nil {
		t.Fatal(err)
	}
	if !match.Matched || match.MessageID != "message-1" || match.PartNo != 1 || match.State != "acked" {
		t.Fatalf("delivery match = %+v", match)
	}
	completed, err := store.GetSMSDeliveryStatus("message-1")
	if err != nil {
		t.Fatal(err)
	}
	if completed.State != "acked" || completed.Parts[0].SIPCode != 202 || completed.Parts[0].ReportAt == nil {
		t.Fatalf("completed delivery result = %+v", completed)
	}
}
