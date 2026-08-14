package db

import (
	"context"
	"errors"
	"time"

	"github.com/yibaiba/hideck/internal/automation"
	"gorm.io/gorm"
)

type AutomaticTaskRecord struct {
	ID           uint64 `gorm:"primaryKey;autoIncrement"`
	Name         string `gorm:"not null"`
	Enabled      bool   `gorm:"not null;index:idx_automatic_tasks_due,priority:1"`
	DeviceID     string `gorm:"not null;index"`
	ProfileICCID string `gorm:"not null;index"`
	ProfileAID   string
	TaskType     string    `gorm:"not null"`
	Environment  string    `gorm:"not null"`
	IntervalDays int       `gorm:"not null"`
	StartDate    string    `gorm:"not null"`
	RunTime      string    `gorm:"not null"`
	Timezone     string    `gorm:"not null"`
	PayloadJSON  string    `gorm:"column:payload_json;not null"`
	RetryCount   int       `gorm:"not null"`
	Notify       bool      `gorm:"not null"`
	NextRunAt    time.Time `gorm:"not null;index:idx_automatic_tasks_due,priority:2"`
	LastRunAt    *time.Time
	LastStatus   string
	LastError    string
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

func (AutomaticTaskRecord) TableName() string { return "automatic_tasks" }

type AutomaticTaskRunRecord struct {
	ID          uint64    `gorm:"primaryKey;autoIncrement"`
	TaskID      uint64    `gorm:"not null;index"`
	DeviceID    string    `gorm:"not null;index"`
	ScheduledAt time.Time `gorm:"not null;index"`
	StartedAt   *time.Time
	FinishedAt  *time.Time
	Status      string `gorm:"not null;index"`
	Attempts    int    `gorm:"not null"`
	Output      string
	Error       string
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

func (AutomaticTaskRunRecord) TableName() string { return "automatic_task_runs" }

type AutomaticTaskStore struct{ database *gorm.DB }

func NewAutomaticTaskStore(database *gorm.DB) *AutomaticTaskStore {
	return &AutomaticTaskStore{database: database}
}

func (s *AutomaticTaskStore) SaveTask(ctx context.Context, task automation.Task) (automation.Task, error) {
	if s == nil || s.database == nil {
		return automation.Task{}, errors.New("automatic task database is unavailable")
	}
	record, err := taskToRecord(task)
	if err != nil {
		return automation.Task{}, err
	}
	now := time.Now().UTC()
	if record.ID == 0 {
		record.CreatedAt = now
		record.UpdatedAt = now
		if err := s.database.WithContext(ctx).Create(&record).Error; err != nil {
			return automation.Task{}, err
		}
		return recordToTask(record)
	}
	err = s.database.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var existing AutomaticTaskRecord
		if err := tx.First(&existing, record.ID).Error; err != nil {
			return mapAutomaticTaskError(err)
		}
		var activeRuns int64
		if err := tx.Model(&AutomaticTaskRunRecord{}).
			Where("task_id = ? AND status IN ?", record.ID, []string{automation.RunStatusQueued, automation.RunStatusRunning}).
			Count(&activeRuns).Error; err != nil {
			return err
		}
		if activeRuns > 0 {
			return automation.ErrTaskBusy
		}
		record.CreatedAt = existing.CreatedAt
		record.LastRunAt = existing.LastRunAt
		record.LastStatus = existing.LastStatus
		record.LastError = existing.LastError
		record.UpdatedAt = now
		return tx.Save(&record).Error
	})
	if err != nil {
		return automation.Task{}, err
	}
	return recordToTask(record)
}

func (s *AutomaticTaskStore) GetTask(ctx context.Context, id uint64) (automation.Task, error) {
	var record AutomaticTaskRecord
	err := s.database.WithContext(ctx).First(&record, id).Error
	if err != nil {
		return automation.Task{}, mapAutomaticTaskError(err)
	}
	return recordToTask(record)
}

func (s *AutomaticTaskStore) ListTasks(ctx context.Context) ([]automation.Task, error) {
	var records []AutomaticTaskRecord
	if err := s.database.WithContext(ctx).Order("created_at DESC").Find(&records).Error; err != nil {
		return nil, err
	}
	tasks := make([]automation.Task, 0, len(records))
	for _, record := range records {
		task, err := recordToTask(record)
		if err != nil {
			return nil, err
		}
		tasks = append(tasks, task)
	}
	return tasks, nil
}

func (s *AutomaticTaskStore) DeleteTask(ctx context.Context, id uint64) error {
	return s.database.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var count int64
		if err := tx.Model(&AutomaticTaskRecord{}).Where("id = ?", id).Count(&count).Error; err != nil {
			return err
		}
		if count == 0 {
			return automation.ErrNotFound
		}
		if err := tx.Model(&AutomaticTaskRunRecord{}).
			Where("task_id = ? AND status IN ?", id, []string{automation.RunStatusQueued, automation.RunStatusRunning}).
			Count(&count).Error; err != nil {
			return err
		}
		if count > 0 {
			return automation.ErrTaskBusy
		}
		if err := tx.Where("task_id = ?", id).Delete(&AutomaticTaskRunRecord{}).Error; err != nil {
			return err
		}
		return tx.Delete(&AutomaticTaskRecord{}, id).Error
	})
}

func mapAutomaticTaskError(err error) error {
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return automation.ErrNotFound
	}
	return err
}
