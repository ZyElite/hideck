package db

import (
	"errors"
	"testing"
	"time"
)

func TestMarkSMSThreadReadPersistsAndPreservesNewerUnreadMessages(t *testing.T) {
	openTestDB(t)
	const (
		iccid = "ICC-READ"
		imsi  = "IMSI-READ"
		peer  = "+10086"
	)
	if err := DB.Create(&SIMCard{ICCID: iccid, IMSI: imsi}).Error; err != nil {
		t.Fatal(err)
	}
	base := time.Date(2026, 8, 13, 18, 0, 0, 0, time.UTC)
	for i, content := range []string{"first", "second", "newer"} {
		if err := SaveSMSForIdentity(SMSRecord{
			Identity: SMSIdentity{ICCID: iccid, IMSI: imsi}, Sender: peer,
			Content: content, Type: 1, Status: 0, Timestamp: base.Add(time.Duration(i) * time.Second),
		}); err != nil {
			t.Fatal(err)
		}
	}

	var boundary SMS
	if err := DB.Where("iccid = ? AND peer = ? AND content = ?", iccid, peer, "second").First(&boundary).Error; err != nil {
		t.Fatal(err)
	}
	result, err := MarkSMSThreadReadByICCID(iccid, peer, boundary.ID)
	if err != nil {
		t.Fatal(err)
	}
	if result.Marked != 2 || result.UnreadCount != 1 {
		t.Fatalf("result=%+v want marked=2 unread=1", result)
	}

	var messages []SMS
	if err := DB.Where("iccid = ? AND peer = ?", iccid, peer).Order("id").Find(&messages).Error; err != nil {
		t.Fatal(err)
	}
	if messages[0].Status != 1 || messages[1].Status != 1 || messages[2].Status != 0 {
		t.Fatalf("statuses=%d,%d,%d want 1,1,0", messages[0].Status, messages[1].Status, messages[2].Status)
	}
	var contact SMSContact
	if err := DB.Where("iccid = ? AND peer = ?", iccid, peer).First(&contact).Error; err != nil {
		t.Fatal(err)
	}
	if contact.UnreadCount != 1 {
		t.Fatalf("UnreadCount=%d want=1", contact.UnreadCount)
	}
}

func TestMarkSMSThreadReadRejectsUnknownOrInvalidScope(t *testing.T) {
	openTestDB(t)
	if _, err := MarkSMSThreadReadByICCID("", "+10086", 1); !errors.Is(err, ErrSMSReadBoundaryInvalid) {
		t.Fatalf("empty ICCID err=%v", err)
	}
	if _, err := MarkSMSThreadReadByICCID("missing", "+10086", 1); !errors.Is(err, ErrSMSNotFound) {
		t.Fatalf("missing thread err=%v", err)
	}

	const iccid = "ICC-READ-BOUNDARY"
	if err := DB.Create(&SIMCard{ICCID: iccid, IMSI: "IMSI-READ-BOUNDARY"}).Error; err != nil {
		t.Fatal(err)
	}
	for _, peer := range []string{"+10010", "+10086"} {
		if err := SaveSMSForIdentity(SMSRecord{
			Identity: SMSIdentity{ICCID: iccid, IMSI: "IMSI-READ-BOUNDARY"},
			Sender:   peer, Content: peer, Type: 1, Status: 0, Timestamp: time.Now(),
		}); err != nil {
			t.Fatal(err)
		}
	}
	var foreign SMS
	if err := DB.Where("iccid = ? AND peer = ?", iccid, "+10010").First(&foreign).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := MarkSMSThreadReadByICCID(iccid, "+10086", foreign.ID); !errors.Is(err, ErrSMSReadBoundaryInvalid) {
		t.Fatalf("foreign boundary err=%v", err)
	}
}

func TestMarkSMSThreadReadUsesSnapshotIDForOutOfOrderTimestamps(t *testing.T) {
	openTestDB(t)
	const (
		iccid = "ICC-READ-ORDER"
		imsi  = "IMSI-READ-ORDER"
		peer  = "+10000"
	)
	if err := DB.Create(&SIMCard{ICCID: iccid, IMSI: imsi}).Error; err != nil {
		t.Fatal(err)
	}
	base := time.Date(2026, 8, 13, 20, 0, 0, 0, time.UTC)
	for _, item := range []struct {
		content string
		at      time.Time
	}{
		{content: "old", at: base},
		{content: "future-inserted-first", at: base.Add(2 * time.Second)},
		{content: "display-boundary", at: base.Add(time.Second)},
		{content: "older-concurrent-insert", at: base.Add(-time.Second)},
	} {
		if err := SaveSMSForIdentity(SMSRecord{
			Identity: SMSIdentity{ICCID: iccid, IMSI: imsi}, Sender: peer,
			Content: item.content, Type: 1, Status: 0, Timestamp: item.at,
		}); err != nil {
			t.Fatal(err)
		}
	}
	var boundary SMS
	if err := DB.Where("content = ?", "display-boundary").First(&boundary).Error; err != nil {
		t.Fatal(err)
	}
	result, err := MarkSMSThreadReadByICCID(iccid, peer, boundary.ID)
	if err != nil {
		t.Fatal(err)
	}
	if result.Marked != 3 || result.UnreadCount != 1 {
		t.Fatalf("result=%+v, want marked=3 unread=1", result)
	}
	var future SMS
	if err := DB.Where("content = ?", "future-inserted-first").First(&future).Error; err != nil {
		t.Fatal(err)
	}
	if future.Status != smsStatusRead {
		t.Fatalf("future status=%d, want read", future.Status)
	}
	var concurrent SMS
	if err := DB.Where("content = ?", "older-concurrent-insert").First(&concurrent).Error; err != nil {
		t.Fatal(err)
	}
	if concurrent.Status != smsStatusUnread {
		t.Fatalf("concurrent status=%d, want unread", concurrent.Status)
	}
}
