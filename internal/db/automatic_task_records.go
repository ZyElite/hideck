package db

import (
	"encoding/json"
	"fmt"

	"github.com/iniwex5/vohive/internal/automation"
)

func taskToRecord(task automation.Task) (AutomaticTaskRecord, error) {
	payload, err := json.Marshal(task.Payload)
	if err != nil {
		return AutomaticTaskRecord{}, fmt.Errorf("encode automatic task payload: %w", err)
	}
	return AutomaticTaskRecord{
		ID: task.ID, Name: task.Name, Enabled: task.Enabled, DeviceID: task.DeviceID,
		ProfileICCID: task.ProfileICCID, ProfileAID: task.ProfileAID, TaskType: task.TaskType,
		Environment: task.Environment, IntervalDays: task.IntervalDays, StartDate: task.StartDate,
		RunTime: task.RunTime, Timezone: task.Timezone, PayloadJSON: string(payload),
		RetryCount: task.RetryCount, Notify: task.Notify, NextRunAt: task.NextRunAt,
		LastRunAt: task.LastRunAt, LastStatus: task.LastStatus, LastError: task.LastError,
		CreatedAt: task.CreatedAt, UpdatedAt: task.UpdatedAt,
	}, nil
}

func recordToTask(record AutomaticTaskRecord) (automation.Task, error) {
	var payload automation.Payload
	if err := json.Unmarshal([]byte(record.PayloadJSON), &payload); err != nil {
		return automation.Task{}, fmt.Errorf("decode automatic task %d payload: %w", record.ID, err)
	}
	return automation.Task{
		ID: record.ID, Name: record.Name, Enabled: record.Enabled, DeviceID: record.DeviceID,
		ProfileICCID: record.ProfileICCID, ProfileAID: record.ProfileAID, TaskType: record.TaskType,
		Environment: record.Environment, IntervalDays: record.IntervalDays, StartDate: record.StartDate,
		RunTime: record.RunTime, Timezone: record.Timezone, Payload: payload,
		RetryCount: record.RetryCount, Notify: record.Notify, NextRunAt: record.NextRunAt,
		LastRunAt: record.LastRunAt, LastStatus: record.LastStatus, LastError: record.LastError,
		CreatedAt: record.CreatedAt, UpdatedAt: record.UpdatedAt,
	}, nil
}

func runToRecord(run automation.Run) AutomaticTaskRunRecord {
	return AutomaticTaskRunRecord{
		ID: run.ID, TaskID: run.TaskID, DeviceID: run.DeviceID, ScheduledAt: run.ScheduledAt,
		StartedAt: run.StartedAt, FinishedAt: run.FinishedAt, Status: run.Status,
		Attempts: run.Attempts, Output: run.Output, Error: run.Error,
		CreatedAt: run.CreatedAt, UpdatedAt: run.UpdatedAt,
	}
}

func runRecordToRun(record AutomaticTaskRunRecord) automation.Run {
	return automation.Run{
		ID: record.ID, TaskID: record.TaskID, DeviceID: record.DeviceID, ScheduledAt: record.ScheduledAt,
		StartedAt: record.StartedAt, FinishedAt: record.FinishedAt, Status: record.Status,
		Attempts: record.Attempts, Output: record.Output, Error: record.Error,
		CreatedAt: record.CreatedAt, UpdatedAt: record.UpdatedAt,
	}
}
