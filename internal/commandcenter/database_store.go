package commandcenter

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	appdb "github.com/yibaiba/hideck/internal/db"
	"gorm.io/gorm"
)

type DatabaseStore struct {
	db *gorm.DB
}

func NewDatabaseStore(database *gorm.DB) *DatabaseStore {
	return &DatabaseStore{db: database}
}

func (s *DatabaseStore) Create(ctx context.Context, execution appdb.CommandExecution, event appdb.CommandEvent) (Event, error) {
	var created Event
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&execution).Error; err != nil {
			return err
		}
		if err := tx.Create(&event).Error; err != nil {
			return err
		}
		decoded, decodeErr := eventFromRecords(event, &execution)
		if decodeErr != nil {
			return decodeErr
		}
		created = decoded
		return nil
	})
	return created, err
}

func (s *DatabaseStore) AddEvent(ctx context.Context, event appdb.CommandEvent) (Event, error) {
	if err := s.db.WithContext(ctx).Create(&event).Error; err != nil {
		return Event{}, err
	}
	var execution appdb.CommandExecution
	if err := s.db.WithContext(ctx).First(&execution, "id = ?", event.ExecutionID).Error; err != nil {
		return Event{}, err
	}
	return eventFromRecords(event, &execution)
}

func (s *DatabaseStore) Finish(ctx context.Context, id, state, message string, at time.Time) error {
	updates := map[string]any{"state": state, "error": message, "completed_at": at, "updated_at": at}
	result := s.db.WithContext(ctx).Model(&appdb.CommandExecution{}).
		Where("id = ? AND state = ?", id, StateRunning).Updates(updates)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return errors.New("命令执行状态已结束: " + id)
	}
	return nil
}

func (s *DatabaseStore) ListEvents(ctx context.Context, after uint64, limit int) ([]Event, error) {
	limit = clampLimit(limit)
	var events []appdb.CommandEvent
	query := s.db.WithContext(ctx).Where("id > ?", after).Order("id asc").Limit(limit)
	if err := query.Find(&events).Error; err != nil {
		return nil, err
	}
	return s.hydrateEvents(ctx, events)
}

func (s *DatabaseStore) ListEventsBefore(ctx context.Context, before uint64, limit int) ([]Event, error) {
	limit = clampLimit(limit)
	query := s.db.WithContext(ctx).Order("id desc").Limit(limit)
	if before > 0 {
		query = query.Where("id < ?", before)
	}
	var records []appdb.CommandEvent
	if err := query.Find(&records).Error; err != nil {
		return nil, err
	}
	reverseEvents(records)
	return s.hydrateEvents(ctx, records)
}

func (s *DatabaseStore) ClearCompleted(ctx context.Context) (int64, error) {
	var deleted int64
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var ids []string
		if err := tx.Model(&appdb.CommandExecution{}).Where("state IN ?", []string{StateCompleted, StateFailed}).Pluck("id", &ids).Error; err != nil {
			return err
		}
		if len(ids) == 0 {
			return nil
		}
		if err := tx.Delete(&appdb.CommandEvent{}, "execution_id IN ?", ids).Error; err != nil {
			return err
		}
		result := tx.Delete(&appdb.CommandExecution{}, "id IN ?", ids)
		deleted = result.RowsAffected
		return result.Error
	})
	return deleted, err
}

func (s *DatabaseStore) hydrateEvents(ctx context.Context, records []appdb.CommandEvent) ([]Event, error) {
	ids := make([]string, 0, len(records))
	for _, record := range records {
		ids = append(ids, record.ExecutionID)
	}
	var executions []appdb.CommandExecution
	if len(ids) > 0 {
		if err := s.db.WithContext(ctx).Where("id IN ?", ids).Find(&executions).Error; err != nil {
			return nil, err
		}
	}
	byID := make(map[string]*appdb.CommandExecution, len(executions))
	for index := range executions {
		byID[executions[index].ID] = &executions[index]
	}
	result := make([]Event, 0, len(records))
	for _, record := range records {
		event, err := eventFromRecords(record, byID[record.ExecutionID])
		if err != nil {
			return nil, err
		}
		result = append(result, event)
	}
	return result, nil
}

func eventFromRecords(record appdb.CommandEvent, execution *appdb.CommandExecution) (Event, error) {
	event := Event{ID: record.ID, ExecutionID: record.ExecutionID, Kind: record.Kind,
		Text: record.Text, Execution: execution, CreatedAt: record.CreatedAt}
	if record.PayloadJSON == "" {
		return event, nil
	}
	var payload eventPayload
	if err := json.Unmarshal([]byte(record.PayloadJSON), &payload); err != nil {
		return Event{}, fmt.Errorf("decode command event %d payload: %w", record.ID, err)
	}
	event.Attachments = payload.Attachments
	return event, nil
}

func clampLimit(limit int) int {
	if limit <= 0 {
		return 100
	}
	if limit > 200 {
		return 200
	}
	return limit
}

func reverseEvents(events []appdb.CommandEvent) {
	for left, right := 0, len(events)-1; left < right; left, right = left+1, right-1 {
		events[left], events[right] = events[right], events[left]
	}
}
