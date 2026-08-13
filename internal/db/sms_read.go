package db

import (
	"errors"
	"strings"
	"time"

	"gorm.io/gorm"
)

var ErrSMSReadBoundaryInvalid = errors.New("sms read boundary is invalid")

const (
	smsTypeIncoming = 1
	smsStatusUnread = 0
	smsStatusRead   = 1
)

type SMSReadResult struct {
	Marked      int64
	UnreadCount int
}

// MarkSMSThreadReadByICCID marks the response snapshot; later inserts stay unread.
func MarkSMSThreadReadByICCID(iccid, peer string, throughID uint) (SMSReadResult, error) {
	iccid = CanonicalICCID(iccid)
	peer = strings.TrimSpace(peer)
	if iccid == "" || peer == "" || throughID == 0 {
		return SMSReadResult{}, ErrSMSReadBoundaryInvalid
	}
	if DB == nil {
		return SMSReadResult{}, errors.New("database is not initialized")
	}

	var result SMSReadResult
	err := DB.Transaction(func(tx *gorm.DB) error {
		var contact SMSContact
		if err := tx.Where("iccid = ? AND peer = ?", iccid, peer).First(&contact).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrSMSNotFound
			}
			return err
		}
		if err := validateSMSReadBoundary(tx, iccid, peer, throughID); err != nil {
			return err
		}

		updated := tx.Model(&SMS{}).
			Where("iccid = ? AND peer = ? AND type = ? AND status = ?", iccid, peer, smsTypeIncoming, smsStatusUnread).
			Where("id <= ?", throughID).
			Update("status", smsStatusRead)
		if updated.Error != nil {
			return updated.Error
		}
		result.Marked = updated.RowsAffected

		var unread int64
		if err := tx.Model(&SMS{}).
			Where("iccid = ? AND peer = ? AND type = ? AND status = ?", iccid, peer, smsTypeIncoming, smsStatusUnread).
			Count(&unread).Error; err != nil {
			return err
		}
		result.UnreadCount = int(unread)
		return tx.Model(&SMSContact{}).
			Where("iccid = ? AND peer = ?", iccid, peer).
			Updates(map[string]any{"unread_count": result.UnreadCount, "updated_at": time.Now()}).Error
	})
	return result, err
}

func validateSMSReadBoundary(tx *gorm.DB, iccid, peer string, throughID uint) error {
	var boundary SMS
	err := tx.Select("id").Where("id = ? AND iccid = ? AND peer = ?", throughID, iccid, peer).First(&boundary).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return ErrSMSReadBoundaryInvalid
	}
	return err
}
