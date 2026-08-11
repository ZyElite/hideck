package db

import (
	"testing"
	"time"
)

func TestMigrateSMSContactIdentityKeyPreservesRowsAndChangesPrimaryKey(t *testing.T) {
	openTestDB(t)
	if err := DB.Migrator().DropTable(&SMSContact{}); err != nil {
		t.Fatal(err)
	}
	legacySchema := `CREATE TABLE sms_contacts (
		imsi text, iccid text, peer text, last_sms_id integer, last_timestamp datetime,
		last_content text, last_type integer, unread_count integer, created_at datetime,
		updated_at datetime, PRIMARY KEY (imsi, peer))`
	if err := DB.Exec(legacySchema).Error; err != nil {
		t.Fatal(err)
	}
	if err := DB.Create(&SIMCard{ICCID: "ICC_MIGRATED", IMSI: "IMSI_MIGRATED", LastSeen: time.Now()}).Error; err != nil {
		t.Fatal(err)
	}
	if err := DB.Exec(`INSERT INTO sms_contacts
		(imsi, peer, last_sms_id, last_timestamp, last_content, last_type, unread_count)
		VALUES (?, ?, ?, ?, ?, ?, ?)`, "IMSI_MIGRATED", "+10086", 3, time.Now(), "kept", 1, 1).Error; err != nil {
		t.Fatal(err)
	}

	if err := MigrateSMSContactIdentityKey(DB); err != nil {
		t.Fatalf("MigrateSMSContactIdentityKey() error=%v", err)
	}
	primary, err := smsContactPrimaryKeyColumns(DB)
	if err != nil {
		t.Fatal(err)
	}
	if len(primary) != 2 || primary[0] != "iccid" || primary[1] != "peer" {
		t.Fatalf("primary key=%v, want [iccid peer]", primary)
	}
	var contact SMSContact
	if err := DB.Where("iccid = ? AND peer = ?", "ICC_MIGRATED", "+10086").First(&contact).Error; err != nil {
		t.Fatal(err)
	}
	if contact.LastContent != "kept" || contact.IMSI != "IMSI_MIGRATED" {
		t.Fatalf("contact=%+v", contact)
	}
}
