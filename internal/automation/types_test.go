package automation

import (
	"errors"
	"testing"
	"time"
)

func TestNormalizeTaskCalculatesNextLocalRun(t *testing.T) {
	now := time.Date(2026, 8, 13, 2, 0, 0, 0, time.UTC)
	task, err := NormalizeTask(Task{
		Name: " daily SMS ", DeviceID: "wwan0", ProfileICCID: "8901F",
		TaskType: TaskTypeSMS, Environment: EnvironmentVoWiFi,
		IntervalDays: 2, StartDate: "2026-08-13", RunTime: "09:00",
		Timezone: "Asia/Shanghai", Payload: Payload{Phone: " 85075 ", Message: "INFO"},
	}, now)
	if err != nil {
		t.Fatalf("NormalizeTask: %v", err)
	}
	if task.Name != "daily SMS" || task.ProfileICCID != "8901" || task.Payload.Phone != "85075" {
		t.Fatalf("task was not normalized: %+v", task)
	}
	want := time.Date(2026, 8, 15, 1, 0, 0, 0, time.UTC)
	if !task.NextRunAt.Equal(want) {
		t.Fatalf("next run = %s, want %s", task.NextRunAt, want)
	}
}

func TestNormalizeTaskAdvancesPastSchedule(t *testing.T) {
	now := time.Date(2026, 8, 13, 3, 0, 0, 0, time.UTC)
	task, err := NormalizeTask(Task{
		Name: "ip", DeviceID: "wwan0", ProfileICCID: "8901",
		TaskType: TaskTypePublicIP, Environment: EnvironmentCellular,
		IntervalDays: 3, StartDate: "2026-08-10", RunTime: "09:00", Timezone: "Asia/Shanghai",
	}, now)
	if err != nil {
		t.Fatalf("NormalizeTask: %v", err)
	}
	want := time.Date(2026, 8, 16, 1, 0, 0, 0, time.UTC)
	if !task.NextRunAt.Equal(want) {
		t.Fatalf("next run = %s, want %s", task.NextRunAt, want)
	}
}

func TestNormalizeTaskRejectsIncompleteActions(t *testing.T) {
	tests := []Task{
		{Name: "sms", DeviceID: "d", ProfileICCID: "1", TaskType: TaskTypeSMS, Environment: EnvironmentVoWiFi, IntervalDays: 1},
		{Name: "call", DeviceID: "d", ProfileICCID: "1", TaskType: TaskTypeCall, Environment: EnvironmentCellular, IntervalDays: 1, Payload: Payload{Phone: "1", HoldSeconds: 61}},
		{Name: "ip", DeviceID: "d", ProfileICCID: "1", TaskType: TaskTypePublicIP, Environment: EnvironmentVoWiFi, IntervalDays: 1},
	}
	for _, task := range tests {
		task.StartDate, task.RunTime, task.Timezone = "2026-08-13", "09:00", "UTC"
		if _, err := NormalizeTask(task, time.Now()); err == nil {
			t.Fatalf("expected validation error for %+v", task)
		}
	}
}

func TestPermanentErrorDisablesRetry(t *testing.T) {
	base := errors.New("failed")
	if !IsRetryable(base) || IsRetryable(Permanent(base)) {
		t.Fatal("retry classification mismatch")
	}
}

func TestNormalizeTaskRejectsDialCommandInjection(t *testing.T) {
	for _, phone := range []string{"1;ATH", "85075\rAT", "tel:+447700900123", "+44 7700"} {
		_, err := NormalizeTask(Task{
			Name: "call", DeviceID: "d", ProfileICCID: "1",
			TaskType: TaskTypeCall, Environment: EnvironmentCellular,
			IntervalDays: 1, StartDate: "2026-08-13", RunTime: "09:00", Timezone: "UTC",
			Payload: Payload{Phone: phone, HoldSeconds: 10},
		}, time.Now())
		if err == nil {
			t.Fatalf("phone %q should be rejected", phone)
		}
	}
}
