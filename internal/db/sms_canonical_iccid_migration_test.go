package db

import (
	"testing"
	"time"
)

func TestMigrateSMSCanonicalICCIDMergesLegacyPaddedIdentity(t *testing.T) {
	openTestDB(t)
	const (
		canonical  = "8944000000000000701"
		padded     = canonical + "F"
		imsi       = "IMSI_CANONICAL_SMS"
		peer       = "+10086"
		secondPeer = "+10010"
	)
	base := time.Date(2026, 8, 13, 12, 0, 0, 0, time.UTC)
	messages := []SMS{
		{ICCID: canonical, IMSI: imsi, Peer: peer, Sender: peer, Content: "first", Type: 1, Status: 0, Timestamp: base},
		{ICCID: padded, IMSI: imsi, Peer: peer, Sender: peer, Content: "latest", Type: 1, Status: 0, Timestamp: base.Add(time.Second)},
		{ICCID: padded, IMSI: imsi, Peer: secondPeer, Sender: secondPeer, Content: "rebuild-canonical-contact", Type: 1, Status: 0, Timestamp: base.Add(2 * time.Second)},
	}
	for index := range messages {
		if err := DB.Create(&messages[index]).Error; err != nil {
			t.Fatal(err)
		}
	}
	contacts := []SMSContact{
		{ICCID: canonical, IMSI: imsi, Peer: peer, LastSMSID: messages[0].ID, LastTimestamp: messages[0].Timestamp, LastContent: messages[0].Content, UnreadCount: 1},
		{ICCID: padded, IMSI: imsi, Peer: peer, LastSMSID: messages[1].ID, LastTimestamp: messages[1].Timestamp, LastContent: messages[1].Content, UnreadCount: 1},
		{ICCID: canonical, IMSI: imsi, Peer: secondPeer, LastSMSID: 0, LastTimestamp: base, LastContent: "stale", UnreadCount: 0},
	}
	if err := DB.Create(&contacts).Error; err != nil {
		t.Fatal(err)
	}
	if err := DB.Create(&SMSDelivery{MessageID: "delivery-canonical", ICCID: padded, IMSI: imsi, Peer: peer}).Error; err != nil {
		t.Fatal(err)
	}

	for run := 0; run < 2; run++ {
		if err := MigrateSMSCanonicalICCID(DB); err != nil {
			t.Fatalf("migration run %d: %v", run+1, err)
		}
	}

	var messageCount int64
	if err := DB.Model(&SMS{}).Where("iccid = ?", canonical).Count(&messageCount).Error; err != nil {
		t.Fatal(err)
	}
	if messageCount != 3 {
		t.Fatalf("canonical message count=%d, want 3", messageCount)
	}
	var merged SMSContact
	if err := DB.Where("iccid = ? AND peer = ?", canonical, peer).First(&merged).Error; err != nil {
		t.Fatal(err)
	}
	if merged.LastContent != "latest" || merged.UnreadCount != 2 {
		t.Fatalf("merged contact=%+v", merged)
	}
	var contactCount int64
	if err := DB.Model(&SMSContact{}).Where("peer = ?", peer).Count(&contactCount).Error; err != nil {
		t.Fatal(err)
	}
	if contactCount != 1 {
		t.Fatalf("contact count=%d, want 1", contactCount)
	}
	var delivery SMSDelivery
	if err := DB.First(&delivery, "message_id = ?", "delivery-canonical").Error; err != nil {
		t.Fatal(err)
	}
	if delivery.ICCID != canonical {
		t.Fatalf("delivery ICCID=%q, want %q", delivery.ICCID, canonical)
	}
	var rebuilt SMSContact
	if err := DB.Where("iccid = ? AND peer = ?", canonical, secondPeer).First(&rebuilt).Error; err != nil {
		t.Fatal(err)
	}
	if rebuilt.LastContent != "rebuild-canonical-contact" || rebuilt.UnreadCount != 1 {
		t.Fatalf("rebuilt canonical contact=%+v", rebuilt)
	}
}
