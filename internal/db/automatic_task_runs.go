package db

import (
	"context"
	"time"

	"github.com/yibaiba/hideck/internal/automation"
	"gorm.io/gorm"
)

func (s *AutomaticTaskStore) ClaimDueRuns(ctx context.Context, now time.Time, limit int) ([]automation.Run, error) {
	var runs []automation.Run
	err := s.database.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var records []AutomaticTaskRecord
		query := tx.Where("enabled = ? AND next_run_at <= ?", true, now).Order("next_run_at ASC")
		if limit > 0 {
			query = query.Limit(limit)
		}
		if err := query.Find(&records).Error; err != nil {
			return err
		}
		for _, record := range records {
			run, claimed, err := claimAutomaticTask(tx, record, now)
			if err != nil {
				return err
			}
			if claimed {
				runs = append(runs, run)
			}
		}
		return nil
	})
	return runs, err
}

func claimAutomaticTask(tx *gorm.DB, record AutomaticTaskRecord, now time.Time) (automation.Run, bool, error) {
	task, err := recordToTask(record)
	if err != nil {
		return automation.Run{}, false, err
	}
	next, err := automation.AdvanceNextRun(task, now)
	if err != nil {
		return automation.Run{}, false, err
	}
	result := tx.Model(&AutomaticTaskRecord{}).
		Where("id = ? AND enabled = ? AND next_run_at = ?", record.ID, true, record.NextRunAt).
		Updates(map[string]any{"next_run_at": next, "updated_at": now})
	if result.Error != nil || result.RowsAffected == 0 {
		return automation.Run{}, false, result.Error
	}
	runRecord := AutomaticTaskRunRecord{
		TaskID: record.ID, DeviceID: record.DeviceID, ScheduledAt: record.NextRunAt,
		Status: automation.RunStatusQueued, CreatedAt: now, UpdatedAt: now,
	}
	if err := tx.Create(&runRecord).Error; err != nil {
		return automation.Run{}, false, err
	}
	if err := updateAutomaticTaskStatus(tx, record.ID, automation.RunStatusQueued, "", now); err != nil {
		return automation.Run{}, false, err
	}
	return runRecordToRun(runRecord), true, nil
}

func (s *AutomaticTaskStore) QueueRun(ctx context.Context, taskID uint64, now time.Time) (automation.Run, error) {
	var record AutomaticTaskRunRecord
	err := s.database.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var task AutomaticTaskRecord
		if err := tx.First(&task, taskID).Error; err != nil {
			return mapAutomaticTaskError(err)
		}
		record = AutomaticTaskRunRecord{
			TaskID: task.ID, DeviceID: task.DeviceID, ScheduledAt: now,
			Status: automation.RunStatusQueued, CreatedAt: now, UpdatedAt: now,
		}
		if err := tx.Create(&record).Error; err != nil {
			return err
		}
		return updateAutomaticTaskStatus(tx, task.ID, automation.RunStatusQueued, "", now)
	})
	if err != nil {
		return automation.Run{}, err
	}
	return runRecordToRun(record), nil
}

func (s *AutomaticTaskStore) UpdateRun(ctx context.Context, run automation.Run) error {
	record := runToRecord(run)
	record.UpdatedAt = time.Now().UTC()
	return s.database.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		result := tx.Model(&AutomaticTaskRunRecord{}).Where("id = ?", run.ID).Updates(map[string]any{
			"started_at": record.StartedAt, "finished_at": record.FinishedAt,
			"status": record.Status, "attempts": record.Attempts, "output": record.Output,
			"error": record.Error, "updated_at": record.UpdatedAt,
		})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return automation.ErrNotFound
		}
		if run.Status != automation.RunStatusSuccess && run.Status != automation.RunStatusFailed {
			return updateAutomaticTaskStatus(tx, run.TaskID, run.Status, run.Error, record.UpdatedAt)
		}
		return tx.Model(&AutomaticTaskRecord{}).Where("id = ?", run.TaskID).Updates(map[string]any{
			"last_run_at": record.FinishedAt, "last_status": run.Status,
			"last_error": run.Error, "updated_at": record.UpdatedAt,
		}).Error
	})
}

func updateAutomaticTaskStatus(tx *gorm.DB, taskID uint64, status, runError string, now time.Time) error {
	return tx.Model(&AutomaticTaskRecord{}).Where("id = ?", taskID).Updates(map[string]any{
		"last_status": status, "last_error": runError, "updated_at": now,
	}).Error
}

func (s *AutomaticTaskStore) RecoverRuns(ctx context.Context, now time.Time) ([]automation.Run, error) {
	var queued []AutomaticTaskRunRecord
	err := s.database.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var running []AutomaticTaskRunRecord
		if err := tx.Where("status = ?", automation.RunStatusRunning).Find(&running).Error; err != nil {
			return err
		}
		for _, record := range running {
			record.Status = automation.RunStatusFailed
			record.Error = "service restarted while the task was running"
			record.FinishedAt = &now
			if err := tx.Model(&AutomaticTaskRunRecord{}).Where("id = ?", record.ID).Updates(record).Error; err != nil {
				return err
			}
			if err := tx.Model(&AutomaticTaskRecord{}).Where("id = ?", record.TaskID).Updates(map[string]any{
				"last_run_at": now, "last_status": record.Status, "last_error": record.Error, "updated_at": now,
			}).Error; err != nil {
				return err
			}
		}
		return tx.Where("status = ?", automation.RunStatusQueued).Order("scheduled_at ASC").Find(&queued).Error
	})
	if err != nil {
		return nil, err
	}
	runs := make([]automation.Run, 0, len(queued))
	for _, record := range queued {
		runs = append(runs, runRecordToRun(record))
	}
	return runs, nil
}

func (s *AutomaticTaskStore) ListRuns(ctx context.Context, taskID uint64, limit, offset int) ([]automation.Run, int64, error) {
	query := s.database.WithContext(ctx).Model(&AutomaticTaskRunRecord{})
	if taskID != 0 {
		query = query.Where("task_id = ?", taskID)
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var records []AutomaticTaskRunRecord
	if err := query.Order("scheduled_at DESC").Limit(limit).Offset(offset).Find(&records).Error; err != nil {
		return nil, 0, err
	}
	runs := make([]automation.Run, 0, len(records))
	for _, record := range records {
		runs = append(runs, runRecordToRun(record))
	}
	return runs, total, nil
}
