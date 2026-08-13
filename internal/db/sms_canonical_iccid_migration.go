package db

import (
	"sort"

	"gorm.io/gorm"
)

type smsContactIdentity struct {
	iccid string
	peer  string
}

type smsRawIdentity struct {
	ICCID string `gorm:"column:iccid"`
	Peer  string `gorm:"column:peer"`
}

// MigrateSMSCanonicalICCID merges legacy BCD-padded ICCIDs into one SMS identity.
func MigrateSMSCanonicalICCID(database *gorm.DB) error {
	if database == nil {
		return nil
	}
	return database.Transaction(func(tx *gorm.DB) error {
		affected, err := canonicalizeSMSMessageRows(tx)
		if err != nil {
			return err
		}
		if err := canonicalizeSMSDeliveryRows(tx); err != nil {
			return err
		}
		return mergeCanonicalSMSContacts(tx, affected)
	})
}

func canonicalizeSMSMessageRows(tx *gorm.DB) (map[smsContactIdentity]struct{}, error) {
	var rows []smsRawIdentity
	if err := tx.Model(&SMS{}).Distinct("iccid", "peer").Find(&rows).Error; err != nil {
		return nil, err
	}
	affected := make(map[smsContactIdentity]struct{})
	updates := make(map[string]string)
	for _, row := range rows {
		canonical := CanonicalICCID(row.ICCID)
		if canonical == "" || canonical == row.ICCID {
			continue
		}
		updates[row.ICCID] = canonical
		if row.Peer != "" {
			affected[smsContactIdentity{iccid: canonical, peer: row.Peer}] = struct{}{}
		}
	}
	for raw, canonical := range updates {
		if err := tx.Model(&SMS{}).Where("iccid = ?", raw).UpdateColumn("iccid", canonical).Error; err != nil {
			return nil, err
		}
	}
	return affected, nil
}

func canonicalizeSMSDeliveryRows(tx *gorm.DB) error {
	var rows []string
	if err := tx.Model(&SMSDelivery{}).Distinct().Pluck("iccid", &rows).Error; err != nil {
		return err
	}
	for _, raw := range rows {
		canonical := CanonicalICCID(raw)
		if canonical == "" || canonical == raw {
			continue
		}
		if err := tx.Model(&SMSDelivery{}).Where("iccid = ?", raw).UpdateColumn("iccid", canonical).Error; err != nil {
			return err
		}
	}
	return nil
}

func mergeCanonicalSMSContacts(tx *gorm.DB, affected map[smsContactIdentity]struct{}) error {
	var contacts []SMSContact
	if err := tx.Find(&contacts).Error; err != nil {
		return err
	}
	groups := make(map[smsContactIdentity][]SMSContact)
	for _, contact := range contacts {
		canonical := CanonicalICCID(contact.ICCID)
		if canonical == "" {
			continue
		}
		key := smsContactIdentity{iccid: canonical, peer: contact.Peer}
		groups[key] = append(groups[key], contact)
	}
	for identity := range affected {
		if _, exists := groups[identity]; !exists {
			groups[identity] = nil
		}
	}
	for identity, group := range groups {
		_, messageChanged := affected[identity]
		if !messageChanged && !smsContactGroupNeedsMerge(identity, group) {
			continue
		}
		if err := replaceCanonicalSMSContact(tx, identity, group); err != nil {
			return err
		}
	}
	return nil
}

func smsContactGroupNeedsMerge(identity smsContactIdentity, group []SMSContact) bool {
	return len(group) > 1 || len(group) == 1 && group[0].ICCID != identity.iccid
}

func replaceCanonicalSMSContact(tx *gorm.DB, identity smsContactIdentity, group []SMSContact) error {
	variants := make([]string, 0, len(group))
	for _, contact := range group {
		variants = append(variants, contact.ICCID)
	}
	if len(variants) > 0 {
		if err := tx.Where("peer = ? AND iccid IN ?", identity.peer, variants).Delete(&SMSContact{}).Error; err != nil {
			return err
		}
	}
	empty, err := rebuildSMSContactTx(tx, identity.iccid, identity.peer)
	if err != nil || !empty {
		return err
	}
	if len(group) == 0 {
		return nil
	}
	sort.SliceStable(group, func(i, j int) bool {
		if group[i].LastTimestamp.Equal(group[j].LastTimestamp) {
			return group[i].LastSMSID > group[j].LastSMSID
		}
		return group[i].LastTimestamp.After(group[j].LastTimestamp)
	})
	winner := group[0]
	winner.ICCID = identity.iccid
	return tx.Create(&winner).Error
}
