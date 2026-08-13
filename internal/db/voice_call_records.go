package db

import (
	"context"
	"time"

	"github.com/iniwex5/vohive/internal/phone"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type VoiceCallRecord struct {
	ID              uint64     `gorm:"primaryKey;autoIncrement"`
	CallID          string     `gorm:"column:call_id;uniqueIndex;not null"`
	DeviceID        string     `gorm:"column:device_id;index;not null"`
	ICCID           string     `gorm:"column:iccid;index"`
	Direction       string     `gorm:"column:direction;index;not null"`
	Peer            string     `gorm:"column:peer;index"`
	Status          string     `gorm:"column:status;index;not null"`
	StartedAt       time.Time  `gorm:"column:started_at;index;not null"`
	AnsweredAt      *time.Time `gorm:"column:answered_at"`
	EndedAt         *time.Time `gorm:"column:ended_at;index"`
	DurationSeconds int64      `gorm:"column:duration_seconds;not null"`
	EndReason       string     `gorm:"column:end_reason"`
	Codec           string     `gorm:"column:codec"`
	RecordingName   string     `gorm:"column:recording_name"`
	PCAPName        string     `gorm:"column:pcap_name"`
	RecordingError  string     `gorm:"column:recording_error"`
	CreatedAt       time.Time  `gorm:"column:created_at"`
	UpdatedAt       time.Time  `gorm:"column:updated_at"`
}

func (VoiceCallRecord) TableName() string { return "voice_call_records" }

type VoiceCallStore struct{ database *gorm.DB }

func NewVoiceCallStore(database *gorm.DB) *VoiceCallStore {
	return &VoiceCallStore{database: database}
}

func (store *VoiceCallStore) Upsert(ctx context.Context, record phone.CallRecord) error {
	model := voiceCallRecordFromDomain(record)
	return store.database.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "call_id"}},
		DoUpdates: clause.AssignmentColumns([]string{
			"device_id", "iccid", "direction", "peer", "status", "started_at", "answered_at",
			"ended_at", "duration_seconds", "end_reason", "codec", "recording_name", "pcap_name",
			"recording_error", "updated_at",
		}),
	}).Create(&model).Error
}

func (store *VoiceCallStore) List(ctx context.Context, limit int) ([]phone.CallRecord, error) {
	var models []VoiceCallRecord
	if err := store.database.WithContext(ctx).Order("started_at desc, id desc").Limit(limit).Find(&models).Error; err != nil {
		return nil, err
	}
	records := make([]phone.CallRecord, 0, len(models))
	for _, model := range models {
		records = append(records, voiceCallRecordToDomain(model))
	}
	return records, nil
}

func voiceCallRecordFromDomain(record phone.CallRecord) VoiceCallRecord {
	return VoiceCallRecord{
		ID: record.ID, CallID: record.CallID, DeviceID: record.DeviceID, ICCID: record.ICCID,
		Direction: record.Direction, Peer: record.Peer, Status: record.Status,
		StartedAt: record.StartedAt, AnsweredAt: record.AnsweredAt, EndedAt: record.EndedAt,
		DurationSeconds: record.DurationSeconds, EndReason: record.EndReason, Codec: record.Codec,
		RecordingName: record.RecordingName, PCAPName: record.PCAPName,
		RecordingError: record.RecordingError,
	}
}

func voiceCallRecordToDomain(record VoiceCallRecord) phone.CallRecord {
	return phone.CallRecord{
		ID: record.ID, CallID: record.CallID, DeviceID: record.DeviceID, ICCID: record.ICCID,
		Direction: record.Direction, Peer: record.Peer, Status: record.Status,
		StartedAt: record.StartedAt, AnsweredAt: record.AnsweredAt, EndedAt: record.EndedAt,
		DurationSeconds: record.DurationSeconds, EndReason: record.EndReason, Codec: record.Codec,
		RecordingName: record.RecordingName, PCAPName: record.PCAPName,
		RecordingError: record.RecordingError,
	}
}
