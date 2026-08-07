package db

import "testing"

func TestEnsureSMSDeliveryPartUniqueIndexReplacesLegacyIndex(t *testing.T) {
	openTestDB(t)
	if err := DB.Exec("DROP INDEX idx_sms_delivery_part_mid_no").Error; err != nil {
		t.Fatal(err)
	}
	if err := DB.Exec(`CREATE INDEX idx_sms_delivery_part_mid_no
		ON sms_delivery_part(message_id, part_no)`).Error; err != nil {
		t.Fatal(err)
	}
	if err := ensureSMSDeliveryPartUniqueIndex(DB); err != nil {
		t.Fatal(err)
	}
	if unique := deliveryPartIndexUnique(t); unique != 1 {
		t.Fatalf("index unique = %d", unique)
	}
}

func TestEnsureSMSDeliveryPartUniqueIndexRejectsDuplicates(t *testing.T) {
	openTestDB(t)
	if err := DB.Exec("DROP INDEX idx_sms_delivery_part_mid_no").Error; err != nil {
		t.Fatal(err)
	}
	if err := DB.Exec(`CREATE INDEX idx_sms_delivery_part_mid_no
		ON sms_delivery_part(message_id, part_no)`).Error; err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := DB.Exec(`INSERT INTO sms_delivery_part
			(message_id, part_no, state, sent_at, created_at, updated_at)
			VALUES ('duplicate', 1, 'pending', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)`).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := ensureSMSDeliveryPartUniqueIndex(DB); err == nil {
		t.Fatal("duplicate delivery parts should reject unique index migration")
	}
	if unique := deliveryPartIndexUnique(t); unique != 0 {
		t.Fatalf("legacy index changed after rejected migration: unique = %d", unique)
	}
}

func deliveryPartIndexUnique(t *testing.T) int {
	t.Helper()
	var indexes []sqliteIndexInfo
	if err := DB.Raw("PRAGMA index_list('sms_delivery_part')").Scan(&indexes).Error; err != nil {
		t.Fatal(err)
	}
	for _, index := range indexes {
		if index.Name == smsDeliveryPartUniqueIndex {
			return index.Unique
		}
	}
	t.Fatal("SMS delivery part index not found")
	return -1
}
