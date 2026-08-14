package db

import (
	"context"
	"errors"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const disclaimerAcceptanceID uint8 = 1

var ErrDisclaimerDatabaseUnavailable = errors.New("disclaimer database is unavailable")

type DisclaimerAcceptance struct {
	ID         uint8     `gorm:"primaryKey;autoIncrement:false"`
	Version    string    `gorm:"not null"`
	AcceptedAt time.Time `gorm:"not null"`
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

type DisclaimerAcceptanceStore struct {
	database *gorm.DB
}

func NewDisclaimerAcceptanceStore(database *gorm.DB) *DisclaimerAcceptanceStore {
	return &DisclaimerAcceptanceStore{database: database}
}

func (store *DisclaimerAcceptanceStore) Status(
	ctx context.Context,
	version string,
) (time.Time, bool, error) {
	if store == nil || store.database == nil {
		return time.Time{}, false, ErrDisclaimerDatabaseUnavailable
	}
	var record DisclaimerAcceptance
	err := store.database.WithContext(ctx).First(&record, disclaimerAcceptanceID).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return time.Time{}, false, nil
	}
	if err != nil {
		return time.Time{}, false, err
	}
	return record.AcceptedAt, record.Version == version, nil
}

func (store *DisclaimerAcceptanceStore) Accept(
	ctx context.Context,
	version string,
	acceptedAt time.Time,
) (time.Time, error) {
	if store == nil || store.database == nil {
		return time.Time{}, ErrDisclaimerDatabaseUnavailable
	}
	acceptedAt = acceptedAt.UTC()
	record := DisclaimerAcceptance{
		ID: disclaimerAcceptanceID, Version: version, AcceptedAt: acceptedAt,
	}
	err := store.database.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "id"}},
		DoUpdates: clause.AssignmentColumns([]string{
			"version", "accepted_at", "updated_at",
		}),
	}).Create(&record).Error
	return acceptedAt, err
}
