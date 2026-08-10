package device

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/iniwex5/vohive/internal/db"
	"github.com/iniwex5/vowifi-go/runtimehost/messaging"
)

var _ messaging.InboundFragmentStore = vowifiDeliveryStore{}
var _ messaging.InboundFragmentLifecycleStore = vowifiDeliveryStore{}

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
	if err := store.MarkSMSDeliveryPartSIPResult("message-1", 1, 202, "pending", "late SIP result", now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	preserved, err := store.GetSMSDeliveryStatus("message-1")
	if err != nil {
		t.Fatal(err)
	}
	if preserved.Parts[0].State != "acked" || preserved.Parts[0].ReportAt == nil || preserved.Parts[0].ErrorText != "" {
		t.Fatalf("late SIP result downgraded report = %+v", preserved.Parts[0])
	}
	assertInboundFragmentStoreRoundTrip(t, store, now)
}

func assertInboundFragmentStoreRoundTrip(t *testing.T, store vowifiDeliveryStore, at time.Time) {
	t.Helper()
	scope := messaging.InboundFragmentScope{
		Owner:      messaging.InboundFragmentOwner{DeviceID: "wwan0", IMSI: "imsi-1"},
		SessionKey: "sender=giffgaff|ref=198",
	}
	result, err := store.SaveInboundFragment(scope, messaging.InboundFragment{
		Reference: 198, ReferenceBits: 8, Total: 2, Sequence: 1,
		Content: "first", ArrivedAt: at, RPMR: 61,
	})
	if err != nil || !result.Inserted || len(result.Fragments) != 1 {
		t.Fatalf("fragment save=%#v err=%v", result, err)
	}
	rows, err := store.LoadInboundFragments(scope.Owner)
	if err != nil || len(rows) != 1 || rows[0].Scope != scope || rows[0].Fragment.Content != "first" {
		t.Fatalf("fragment load=%#v err=%v", rows, err)
	}
	degradedAt := at.Add(time.Second)
	if err := store.MarkInboundFragmentsDegraded(scope, degradedAt); err != nil {
		t.Fatal(err)
	}
	rows, err = store.LoadInboundFragments(scope.Owner)
	if err != nil || len(rows) != 1 || !rows[0].Fragment.DegradedAt.Equal(degradedAt) {
		t.Fatalf("fragment degraded state=%#v err=%v", rows, err)
	}
	if err := store.DeleteInboundFragments(scope); err != nil {
		t.Fatal(err)
	}
}
