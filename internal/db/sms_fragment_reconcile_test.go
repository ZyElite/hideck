package db

import (
	"testing"
	"time"
)

func TestSaveReceivedMultipartSMSReconcilesDegradedRecord(t *testing.T) {
	openTestDB(t)
	if err := DB.Create(&SIMCard{ICCID: "ICC-FRAGMENT", IMSI: "IMSI-FRAGMENT"}).Error; err != nil {
		t.Fatal(err)
	}
	degradedAt := time.Date(2026, 8, 10, 14, 30, 5, 0, time.UTC)
	input := ReceivedMultipartSMS{
		IMSI: "IMSI-FRAGMENT", LocalPhone: "+447840844894",
		Sender: "giffgaff", Recipient: "+447840844894",
		Content:            "[incomplete 1/2 missing=2] Hi there.",
		FragmentSessionKey: "sender=giffgaff|ref=212", Timestamp: degradedAt,
		Incomplete: true,
	}
	degraded, err := SaveReceivedMultipartSMS(input)
	if err != nil || !degraded.Created || degraded.Reconciled || degraded.Duplicate {
		t.Fatalf("degraded result=%+v err=%v", degraded, err)
	}

	input.Content = "Hi there. Complete message."
	input.Timestamp = degradedAt.Add(3 * time.Minute)
	input.Incomplete = false
	complete, err := SaveReceivedMultipartSMS(input)
	if err != nil || complete.Created || !complete.Reconciled || complete.Duplicate {
		t.Fatalf("complete result=%+v err=%v", complete, err)
	}
	if complete.SMSID != degraded.SMSID {
		t.Fatalf("complete SMS ID=%d want degraded ID=%d", complete.SMSID, degraded.SMSID)
	}
	assertReconciledMultipartSMS(t, degraded.SMSID, degradedAt)
}

func assertReconciledMultipartSMS(t *testing.T, smsID uint, originalTimestamp time.Time) {
	t.Helper()
	var messages []SMS
	if err := DB.Where("imsi = ?", "IMSI-FRAGMENT").Find(&messages).Error; err != nil {
		t.Fatal(err)
	}
	if len(messages) != 1 {
		t.Fatalf("SMS rows=%d want 1: %+v", len(messages), messages)
	}
	sms := messages[0]
	if sms.ID != smsID || sms.Content != "Hi there. Complete message." || sms.Incomplete {
		t.Fatalf("reconciled SMS=%+v", sms)
	}
	if !sms.Timestamp.Equal(originalTimestamp) {
		t.Fatalf("timestamp=%v want original degraded timestamp=%v", sms.Timestamp, originalTimestamp)
	}
	var contact SMSContact
	if err := DB.Where("imsi = ? AND peer = ?", "IMSI-FRAGMENT", "giffgaff").First(&contact).Error; err != nil {
		t.Fatal(err)
	}
	if contact.LastSMSID != smsID || contact.LastContent != sms.Content || contact.UnreadCount != 1 {
		t.Fatalf("contact=%+v", contact)
	}
}

func TestSaveReceivedMultipartSMSKeepsOtherSessionsIndependent(t *testing.T) {
	openTestDB(t)
	base := ReceivedMultipartSMS{
		IMSI: "IMSI-INDEPENDENT", Sender: "giffgaff", Content: "part one",
		Recipient: "+447840844894", LocalPhone: "+447840844894",
		Timestamp: time.Now(), Incomplete: true,
	}
	for _, key := range []string{"session-a", "session-b"} {
		base.FragmentSessionKey = key
		if _, err := SaveReceivedMultipartSMS(base); err != nil {
			t.Fatal(err)
		}
	}
	var count int64
	if err := DB.Model(&SMS{}).Where("imsi = ?", base.IMSI).Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Fatalf("independent session rows=%d want 2", count)
	}
}
