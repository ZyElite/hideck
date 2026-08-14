package db

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/yibaiba/hideck/internal/phone"
	"gorm.io/gorm"
)

func TestVoiceCallStoreUpsertsAndListsNewestFirst(t *testing.T) {
	database, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "calls.db")), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := database.AutoMigrate(&VoiceCallRecord{}); err != nil {
		t.Fatal(err)
	}
	store := NewVoiceCallStore(database)
	ctx := context.Background()
	older := time.Now().Add(-time.Minute).UTC()
	newer := time.Now().UTC()
	if err := store.Upsert(ctx, phone.CallRecord{
		CallID: "call-old", DeviceID: "dev-1", Direction: "inbound",
		Status: phone.StatusRinging, StartedAt: older,
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.Upsert(ctx, phone.CallRecord{
		CallID: "call-new", DeviceID: "dev-2", ICCID: "iccid-2", Direction: "outbound",
		Peer: "888", Status: phone.StatusCalling, StartedAt: newer,
	}); err != nil {
		t.Fatal(err)
	}
	endedAt := newer.Add(5 * time.Second)
	if err := store.Upsert(ctx, phone.CallRecord{
		CallID: "call-new", DeviceID: "dev-2", ICCID: "iccid-2", Direction: "outbound",
		Peer: "888", Status: phone.StatusCompleted, StartedAt: newer, AnsweredAt: &newer,
		EndedAt: &endedAt, DurationSeconds: 5, EndReason: "local_hangup", Codec: "PCMU",
		RecordingName: "call-new.mp3", PCAPName: "call-new.pcap",
	}); err != nil {
		t.Fatal(err)
	}
	records, err := store.List(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 2 || records[0].CallID != "call-new" || records[1].CallID != "call-old" {
		t.Fatalf("records = %+v", records)
	}
	if records[0].Status != phone.StatusCompleted || records[0].RecordingName != "call-new.mp3" || records[0].PCAPName != "call-new.pcap" {
		t.Fatalf("updated record = %+v", records[0])
	}
}
