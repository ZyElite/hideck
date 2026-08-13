package db

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/iniwex5/vohive/internal/automation"
)

func TestAutomaticTaskStoreClaimsDueTaskOnce(t *testing.T) {
	openTestDB(t)
	store := NewAutomaticTaskStore(DB)
	now := time.Date(2026, 8, 13, 12, 0, 0, 0, time.UTC)
	task, err := store.SaveTask(context.Background(), automation.Task{
		Name: "daily", Enabled: true, DeviceID: "wwan0", ProfileICCID: "8901",
		TaskType: automation.TaskTypePublicIP, Environment: automation.EnvironmentCellular,
		IntervalDays: 1, StartDate: "2026-08-01", RunTime: "09:00", Timezone: "UTC",
		NextRunAt: now.Add(-time.Minute),
	})
	if err != nil {
		t.Fatalf("SaveTask: %v", err)
	}
	runs, err := store.ClaimDueRuns(context.Background(), now, 10)
	if err != nil {
		t.Fatalf("ClaimDueRuns: %v", err)
	}
	if len(runs) != 1 || runs[0].TaskID != task.ID {
		t.Fatalf("runs = %+v", runs)
	}
	again, err := store.ClaimDueRuns(context.Background(), now, 10)
	if err != nil || len(again) != 0 {
		t.Fatalf("second claim = %+v, %v", again, err)
	}
	updated, err := store.GetTask(context.Background(), task.ID)
	if err != nil || !updated.NextRunAt.After(now) {
		t.Fatalf("next run was not advanced: %+v, %v", updated, err)
	}
}

func TestAutomaticTaskStoreRecoversInterruptedRuns(t *testing.T) {
	openTestDB(t)
	store := NewAutomaticTaskStore(DB)
	task, err := store.SaveTask(context.Background(), automation.Task{
		Name: "sms", DeviceID: "wwan0", ProfileICCID: "8901",
		TaskType: automation.TaskTypeSMS, Environment: automation.EnvironmentVoWiFi,
		IntervalDays: 1, StartDate: "2026-08-13", RunTime: "09:00", Timezone: "UTC",
		Payload: automation.Payload{Phone: "85075", Message: "INFO"}, NextRunAt: time.Now().Add(time.Hour),
	})
	if err != nil {
		t.Fatalf("SaveTask: %v", err)
	}
	running, err := store.QueueRun(context.Background(), task.ID, time.Now())
	if err != nil {
		t.Fatalf("QueueRun: %v", err)
	}
	running.Status = automation.RunStatusRunning
	if err := store.UpdateRun(context.Background(), running); err != nil {
		t.Fatalf("UpdateRun: %v", err)
	}
	if _, err := store.RecoverRuns(context.Background(), time.Now()); err != nil {
		t.Fatalf("RecoverRuns: %v", err)
	}
	runs, _, err := store.ListRuns(context.Background(), task.ID, 10, 0)
	if err != nil || len(runs) != 1 || runs[0].Status != automation.RunStatusFailed {
		t.Fatalf("recovered runs = %+v, %v", runs, err)
	}
}

func TestAutomaticTaskStoreRejectsDeleteWhileRunQueued(t *testing.T) {
	openTestDB(t)
	store := NewAutomaticTaskStore(DB)
	task, err := store.SaveTask(context.Background(), automation.Task{
		Name: "ip", DeviceID: "wwan0", ProfileICCID: "8901",
		TaskType: automation.TaskTypePublicIP, Environment: automation.EnvironmentCellular,
		IntervalDays: 1, StartDate: "2026-08-13", RunTime: "09:00", Timezone: "UTC",
		NextRunAt: time.Now().Add(time.Hour),
	})
	if err != nil {
		t.Fatalf("SaveTask: %v", err)
	}
	if _, err := store.QueueRun(context.Background(), task.ID, time.Now()); err != nil {
		t.Fatalf("QueueRun: %v", err)
	}
	if err := store.DeleteTask(context.Background(), task.ID); !errors.Is(err, automation.ErrTaskBusy) {
		t.Fatalf("DeleteTask error = %v, want ErrTaskBusy", err)
	}
}
