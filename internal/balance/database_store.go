package balance

import (
	"context"
	"errors"
	"time"

	appdb "github.com/iniwex5/vohive/internal/db"
	"gorm.io/gorm"
)

type DatabaseStore struct {
	db *gorm.DB
}

func NewDatabaseStore(database *gorm.DB) *DatabaseStore {
	return &DatabaseStore{db: database}
}

func (s *DatabaseStore) CreatePending(ctx context.Context, query Query) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var count int64
		err := tx.Model(&appdb.BalanceQuery{}).
			Where("iccid = ? AND state IN ?", query.ICCID, pendingStates()).Count(&count).Error
		if err != nil {
			return err
		}
		if count > 0 {
			return ErrPendingQuery
		}
		return tx.Create(toRecord(query)).Error
	})
}

func (s *DatabaseStore) SaveManual(ctx context.Context, query Query) error {
	return s.db.WithContext(ctx).Save(toRecord(query)).Error
}

func (s *DatabaseStore) DeleteManual(ctx context.Context, deviceID string) (bool, error) {
	result := s.db.WithContext(ctx).Where(
		"id = ? AND device_id = ? AND transport = ?", manualBalanceID(deviceID), deviceID, TransportManual,
	).Delete(&appdb.BalanceQuery{})
	return result.RowsAffected == 1, result.Error
}

func (s *DatabaseStore) MarkAwaitingReply(ctx context.Context, id string, now time.Time) error {
	result := s.db.WithContext(ctx).Model(&appdb.BalanceQuery{}).
		Where("id = ? AND state = ?", id, StateSending).
		Updates(map[string]any{"state": StateAwaitingReply, "updated_at": now})
	return transitionError(result, id)
}

func (s *DatabaseStore) MarkFailed(ctx context.Context, id string, cause error, now time.Time) error {
	message := ""
	if cause != nil {
		message = cause.Error()
	}
	result := s.db.WithContext(ctx).Model(&appdb.BalanceQuery{}).
		Where("id = ? AND state IN ?", id, pendingStates()).
		Updates(map[string]any{"state": StateFailed, "error": message, "completed_at": now, "updated_at": now})
	return transitionError(result, id)
}

func (s *DatabaseStore) Complete(ctx context.Context, id string, completion Completion, now time.Time) (bool, error) {
	updates := map[string]any{"state": StateCompleted, "parse_state": completion.ParseState,
		"amount": completion.Amount, "currency": completion.Currency, "summary": completion.Summary,
		"raw_response": completion.RawResponse, "response_sms_id": completion.SMSID,
		"completed_at": now, "updated_at": now}
	result := s.db.WithContext(ctx).Model(&appdb.BalanceQuery{}).
		Where("id = ? AND state IN ?", id, pendingStates()).Updates(updates)
	return result.RowsAffected == 1, result.Error
}

func (s *DatabaseStore) FindPending(ctx context.Context, iccid string, at time.Time) (Query, bool, error) {
	var record appdb.BalanceQuery
	err := s.db.WithContext(ctx).Where(
		"iccid = ? AND state IN ? AND julianday(started_at) <= julianday(?) AND julianday(expires_at) >= julianday(?)",
		iccid, pendingStates(), at, at,
	).Order("started_at desc").First(&record).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return Query{}, false, nil
	}
	return fromRecord(record), err == nil, err
}

func (s *DatabaseStore) ExpirePending(ctx context.Context, now time.Time) (int64, error) {
	result := s.db.WithContext(ctx).Model(&appdb.BalanceQuery{}).
		Where("state IN ? AND expires_at <= ?", pendingStates(), now).
		Updates(map[string]any{"state": StateTimedOut, "error": "等待运营商回复超时",
			"completed_at": now, "updated_at": now})
	return result.RowsAffected, result.Error
}

func (s *DatabaseStore) Get(ctx context.Context, id string) (Query, bool, error) {
	var record appdb.BalanceQuery
	err := s.db.WithContext(ctx).First(&record, "id = ?", id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return Query{}, false, nil
	}
	return fromRecord(record), err == nil, err
}

func (s *DatabaseStore) List(ctx context.Context, deviceID string, limit int, before *time.Time) ([]Query, error) {
	if limit <= 0 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}
	query := s.db.WithContext(ctx).Model(&appdb.BalanceQuery{})
	if deviceID != "" {
		query = query.Where("device_id = ?", deviceID)
	}
	if before != nil && !before.IsZero() {
		query = query.Where("created_at < ?", *before)
	}
	var records []appdb.BalanceQuery
	if err := query.Order("created_at desc, id desc").Limit(limit).Find(&records).Error; err != nil {
		return nil, err
	}
	result := make([]Query, 0, len(records))
	for _, record := range records {
		result = append(result, fromRecord(record))
	}
	return result, nil
}

func transitionError(result *gorm.DB, id string) error {
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return errors.New("余额查询状态已变化: " + id)
	}
	return nil
}

func pendingStates() []string {
	return []string{StateSending, StateAwaitingReply}
}

func toRecord(query Query) *appdb.BalanceQuery {
	return &appdb.BalanceQuery{ID: query.ID, DeviceID: query.DeviceID, ICCID: query.ICCID,
		RuleID: query.RuleID, Transport: query.Transport, State: query.State, ParseState: query.ParseState,
		Amount: query.Amount, Currency: query.Currency, Summary: query.Summary, RawResponse: query.RawResponse,
		ResponseSMSID: query.ResponseSMSID, StartedAt: query.StartedAt, ExpiresAt: query.ExpiresAt,
		CompletedAt: query.CompletedAt, Error: query.Error, CreatedAt: query.CreatedAt, UpdatedAt: query.UpdatedAt}
}

func fromRecord(record appdb.BalanceQuery) Query {
	return Query{ID: record.ID, DeviceID: record.DeviceID, ICCID: record.ICCID, RuleID: record.RuleID,
		Transport: record.Transport, State: record.State, ParseState: record.ParseState, Amount: record.Amount,
		Currency: record.Currency, Summary: record.Summary, RawResponse: record.RawResponse,
		ResponseSMSID: record.ResponseSMSID, StartedAt: record.StartedAt, ExpiresAt: record.ExpiresAt,
		CompletedAt: record.CompletedAt, Error: record.Error, CreatedAt: record.CreatedAt, UpdatedAt: record.UpdatedAt}
}
