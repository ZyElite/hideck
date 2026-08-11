package db

import (
	"errors"
	"strings"
	"time"

	"gorm.io/gorm"
)

const smsFragmentSessionUniqueIndexSQL = "CREATE UNIQUE INDEX IF NOT EXISTS uidx_sms_fragment_session " +
	"ON sms(imsi, fragment_session_key) WHERE fragment_session_key <> ''"

type ReceivedMultipartSMS struct {
	IMSI               string
	LocalPhone         string
	Sender             string
	Recipient          string
	Content            string
	FragmentSessionKey string
	Timestamp          time.Time
	Incomplete         bool
	iccid              string
}

type ReceivedMultipartSMSResult struct {
	SMSID      uint
	Created    bool
	Reconciled bool
	Duplicate  bool
}

func ensureSMSFragmentSessionUniqueIndex(database *gorm.DB) error {
	if database == nil {
		return errors.New("database not initialized")
	}
	return database.Exec(smsFragmentSessionUniqueIndexSQL).Error
}

func SaveReceivedMultipartSMS(input ReceivedMultipartSMS) (ReceivedMultipartSMSResult, error) {
	if DB == nil {
		return ReceivedMultipartSMSResult{}, errors.New("database not initialized")
	}
	normalized, err := normalizeReceivedMultipartSMS(input)
	if err != nil {
		return ReceivedMultipartSMSResult{}, err
	}
	var result ReceivedMultipartSMSResult
	err = DB.Transaction(func(tx *gorm.DB) error {
		var existing SMS
		findErr := tx.Where(
			"imsi = ? AND fragment_session_key = ?",
			normalized.IMSI, normalized.FragmentSessionKey,
		).First(&existing).Error
		if errors.Is(findErr, gorm.ErrRecordNotFound) {
			created, createErr := createReceivedMultipartSMS(tx, normalized)
			result = created
			return createErr
		}
		if findErr != nil {
			return findErr
		}
		reconciled, reconcileErr := reconcileReceivedMultipartSMS(tx, existing, normalized)
		result = reconciled
		return reconcileErr
	})
	return result, err
}

func normalizeReceivedMultipartSMS(input ReceivedMultipartSMS) (ReceivedMultipartSMS, error) {
	result := input
	result.IMSI = strings.TrimSpace(input.IMSI)
	result.LocalPhone = strings.TrimSpace(input.LocalPhone)
	result.Sender = strings.TrimSpace(input.Sender)
	result.Recipient = strings.TrimSpace(input.Recipient)
	result.Content = strings.TrimSpace(input.Content)
	result.FragmentSessionKey = strings.TrimSpace(input.FragmentSessionKey)
	if result.IMSI == "" || result.Sender == "" || result.Content == "" || result.FragmentSessionKey == "" {
		return ReceivedMultipartSMS{}, errors.New("invalid received multipart SMS")
	}
	if result.Timestamp.IsZero() {
		return ReceivedMultipartSMS{}, errors.New("missing received multipart SMS timestamp")
	}
	result.LocalPhone = normalizeSMSLocalPhone(
		result.IMSI, 1, result.LocalPhone, result.Sender, result.Recipient,
	)
	result.iccid = GetICCIDForIMSI(result.IMSI)
	return result, nil
}

func createReceivedMultipartSMS(
	tx *gorm.DB,
	input ReceivedMultipartSMS,
) (ReceivedMultipartSMSResult, error) {
	sms := SMS{
		IMSI: input.IMSI, ICCID: input.iccid,
		Peer:       normalizeSMSPeer(1, input.Sender, input.Recipient),
		LocalPhone: input.LocalPhone,
		Sender:     input.Sender, Recipient: input.Recipient, Content: input.Content,
		Type: 1, Status: 0, Timestamp: input.Timestamp.Truncate(time.Second),
		FragmentSessionKey: input.FragmentSessionKey, Incomplete: input.Incomplete,
	}
	if err := tx.Create(&sms).Error; err != nil {
		return ReceivedMultipartSMSResult{}, err
	}
	if sms.Peer != "" {
		if err := upsertSMSContactFromSMS(tx, &sms); err != nil {
			return ReceivedMultipartSMSResult{}, err
		}
	}
	return ReceivedMultipartSMSResult{SMSID: sms.ID, Created: true}, nil
}

func reconcileReceivedMultipartSMS(
	tx *gorm.DB,
	existing SMS,
	input ReceivedMultipartSMS,
) (ReceivedMultipartSMSResult, error) {
	if !existing.Incomplete || input.Incomplete {
		if strings.TrimSpace(existing.Content) != input.Content && existing.Incomplete == input.Incomplete {
			return ReceivedMultipartSMSResult{}, errors.New("received multipart SMS session content conflict")
		}
		return ReceivedMultipartSMSResult{SMSID: existing.ID, Duplicate: true}, nil
	}
	if err := tx.Model(&existing).Updates(map[string]any{
		"content": input.Content, "incomplete": false,
	}).Error; err != nil {
		return ReceivedMultipartSMSResult{}, err
	}
	if _, err := rebuildSMSContactTx(tx, existing.ICCID, existing.Peer); err != nil {
		return ReceivedMultipartSMSResult{}, err
	}
	return ReceivedMultipartSMSResult{SMSID: existing.ID, Reconciled: true}, nil
}
