package db

import (
	"fmt"
	"sort"
	"strings"

	"gorm.io/gorm"
)

const smsContactsIdentityMigrationTable = "sms_contacts_identity_v2"

func MigrateSMSContactIdentityKey(database *gorm.DB) error {
	if database == nil || !database.Migrator().HasTable("sms_contacts") {
		return nil
	}
	pkColumns, err := smsContactPrimaryKeyColumns(database)
	if err != nil {
		return err
	}
	if strings.Join(pkColumns, ",") == "iccid,peer" {
		return nil
	}

	return database.Transaction(func(tx *gorm.DB) error {
		if !tx.Migrator().HasColumn("sms_contacts", "iccid") {
			if err := tx.Exec("ALTER TABLE sms_contacts ADD COLUMN iccid text").Error; err != nil {
				return err
			}
		}
		if err := backfillSMSContactICCID(tx); err != nil {
			return err
		}
		return rebuildSMSContactIdentityTable(tx)
	})
}

func smsContactPrimaryKeyColumns(database *gorm.DB) ([]string, error) {
	type columnInfo struct {
		Name string `gorm:"column:name"`
		PK   int    `gorm:"column:pk"`
	}
	var columns []columnInfo
	if err := database.Raw("PRAGMA table_info('sms_contacts')").Scan(&columns).Error; err != nil {
		return nil, err
	}
	primary := make([]string, 0, 2)
	for _, column := range columns {
		if column.PK > 0 {
			primary = append(primary, column.Name)
		}
	}
	sort.Strings(primary)
	return primary, nil
}

func backfillSMSContactICCID(tx *gorm.DB) error {
	statement := `UPDATE sms_contacts
		SET iccid = COALESCE(
			(SELECT sc.iccid FROM sim_cards sc
			 WHERE sc.imsi = sms_contacts.imsi
			   AND TRIM(sc.iccid) <> '' AND sc.iccid NOT LIKE 'reader-imsi-%'
			 ORDER BY sc.last_seen DESC LIMIT 1),
			CASE WHEN TRIM(COALESCE(imsi, '')) <> '' THEN 'imsi:' || TRIM(imsi)
			     ELSE 'legacy-contact:' || rowid END)
		WHERE TRIM(COALESCE(iccid, '')) = ''`
	return tx.Exec(statement).Error
}

func rebuildSMSContactIdentityTable(tx *gorm.DB) error {
	if err := tx.Exec("DROP TABLE IF EXISTS " + smsContactsIdentityMigrationTable).Error; err != nil {
		return err
	}
	create := fmt.Sprintf(`CREATE TABLE %s (
		imsi text, iccid text NOT NULL, peer text NOT NULL, last_sms_id integer,
		last_timestamp datetime, last_content text, last_type integer,
		unread_count integer, created_at datetime, updated_at datetime,
		PRIMARY KEY (iccid, peer))`, smsContactsIdentityMigrationTable)
	if err := tx.Exec(create).Error; err != nil {
		return err
	}
	copyRows := fmt.Sprintf(`INSERT INTO %s
		(imsi, iccid, peer, last_sms_id, last_timestamp, last_content, last_type, unread_count, created_at, updated_at)
		SELECT imsi, iccid, peer, last_sms_id, last_timestamp, last_content, last_type, unread_count, created_at, updated_at
		FROM (SELECT *, ROW_NUMBER() OVER (
			PARTITION BY iccid, peer ORDER BY last_timestamp DESC, last_sms_id DESC
		) AS identity_rank FROM sms_contacts) WHERE identity_rank = 1`, smsContactsIdentityMigrationTable)
	if err := tx.Exec(copyRows).Error; err != nil {
		return err
	}
	if err := tx.Exec("DROP TABLE sms_contacts").Error; err != nil {
		return err
	}
	return tx.Exec("ALTER TABLE " + smsContactsIdentityMigrationTable + " RENAME TO sms_contacts").Error
}
