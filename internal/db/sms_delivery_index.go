package db

import (
	"errors"
	"fmt"

	"gorm.io/gorm"
)

const smsDeliveryPartUniqueIndex = "idx_sms_delivery_part_mid_no"

type sqliteIndexInfo struct {
	Name   string `gorm:"column:name"`
	Unique int    `gorm:"column:unique"`
}

func ensureSMSDeliveryPartUniqueIndex(database *gorm.DB) error {
	if database == nil {
		return errors.New("sms delivery part index: database is nil")
	}
	var indexes []sqliteIndexInfo
	if err := database.Raw("PRAGMA index_list('sms_delivery_part')").Scan(&indexes).Error; err != nil {
		return fmt.Errorf("inspect SMS delivery part indexes: %w", err)
	}
	for _, index := range indexes {
		if index.Name != smsDeliveryPartUniqueIndex || index.Unique == 1 {
			continue
		}
		return replaceSMSDeliveryPartIndex(database)
	}
	return nil
}

func replaceSMSDeliveryPartIndex(database *gorm.DB) error {
	var duplicateGroups int64
	err := database.Raw(`SELECT COUNT(*) FROM (
		SELECT 1 FROM sms_delivery_part GROUP BY message_id, part_no HAVING COUNT(*) > 1
	)`).Scan(&duplicateGroups).Error
	if err != nil {
		return fmt.Errorf("inspect duplicate SMS delivery parts: %w", err)
	}
	if duplicateGroups > 0 {
		return fmt.Errorf("cannot create SMS delivery part unique index: %d duplicate message/part groups", duplicateGroups)
	}
	return database.Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec("DROP INDEX idx_sms_delivery_part_mid_no").Error; err != nil {
			return fmt.Errorf("drop non-unique SMS delivery part index: %w", err)
		}
		if err := tx.Exec(`CREATE UNIQUE INDEX idx_sms_delivery_part_mid_no
			ON sms_delivery_part(message_id, part_no)`).Error; err != nil {
			return fmt.Errorf("create SMS delivery part unique index: %w", err)
		}
		return nil
	})
}
