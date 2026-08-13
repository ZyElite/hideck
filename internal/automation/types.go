package automation

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

const (
	TaskTypeSMS      = "sms"
	TaskTypeCall     = "call"
	TaskTypePublicIP = "public_ip"

	EnvironmentVoWiFi   = "vowifi"
	EnvironmentCellular = "cellular"

	RunStatusQueued  = "queued"
	RunStatusRunning = "running"
	RunStatusSuccess = "success"
	RunStatusFailed  = "failed"

	minIntervalDays = 1
	maxIntervalDays = 365
	maxRetryCount   = 10
	maxCallSeconds  = 60
)

var (
	ErrNotFound    = errors.New("automatic task not found")
	ErrNotStarted  = errors.New("automatic task scheduler is not running")
	ErrTaskBusy    = errors.New("automatic task has queued or running executions")
	ErrInvalidTask = errors.New("invalid automatic task")
)

type Payload struct {
	Phone       string `json:"phone,omitempty"`
	Message     string `json:"message,omitempty"`
	HoldSeconds int    `json:"hold_seconds,omitempty"`
}

type Task struct {
	ID           uint64     `json:"id"`
	Name         string     `json:"name"`
	Enabled      bool       `json:"enabled"`
	DeviceID     string     `json:"device_id"`
	ProfileICCID string     `json:"profile_iccid"`
	ProfileAID   string     `json:"profile_aid,omitempty"`
	TaskType     string     `json:"task_type"`
	Environment  string     `json:"environment"`
	IntervalDays int        `json:"interval_days"`
	StartDate    string     `json:"start_date"`
	RunTime      string     `json:"run_time"`
	Timezone     string     `json:"timezone"`
	Payload      Payload    `json:"payload"`
	RetryCount   int        `json:"retry_count"`
	Notify       bool       `json:"notify"`
	NextRunAt    time.Time  `json:"next_run_at"`
	LastRunAt    *time.Time `json:"last_run_at,omitempty"`
	LastStatus   string     `json:"last_status,omitempty"`
	LastError    string     `json:"last_error,omitempty"`
	CreatedAt    time.Time  `json:"created_at"`
	UpdatedAt    time.Time  `json:"updated_at"`
}

type Run struct {
	ID          uint64     `json:"id"`
	TaskID      uint64     `json:"task_id"`
	DeviceID    string     `json:"device_id"`
	ScheduledAt time.Time  `json:"scheduled_at"`
	StartedAt   *time.Time `json:"started_at,omitempty"`
	FinishedAt  *time.Time `json:"finished_at,omitempty"`
	Status      string     `json:"status"`
	Attempts    int        `json:"attempts"`
	Output      string     `json:"output,omitempty"`
	Error       string     `json:"error,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
}

func NormalizeTask(input Task, now time.Time) (Task, error) {
	task := input
	task.Name = strings.TrimSpace(task.Name)
	task.DeviceID = strings.TrimSpace(task.DeviceID)
	task.ProfileICCID = canonicalICCID(task.ProfileICCID)
	task.ProfileAID = strings.TrimSpace(task.ProfileAID)
	task.TaskType = strings.ToLower(strings.TrimSpace(task.TaskType))
	task.Environment = strings.ToLower(strings.TrimSpace(task.Environment))
	task.StartDate = strings.TrimSpace(task.StartDate)
	task.RunTime = strings.TrimSpace(task.RunTime)
	task.Timezone = strings.TrimSpace(task.Timezone)
	task.Payload.Phone = strings.TrimSpace(task.Payload.Phone)
	if err := validateTask(task); err != nil {
		return Task{}, err
	}
	next, err := CalculateNextRun(task, now)
	if err != nil {
		return Task{}, err
	}
	task.NextRunAt = next
	return task, nil
}

func validateTask(task Task) error {
	switch {
	case task.Name == "":
		return errors.New("task name is required")
	case task.DeviceID == "":
		return errors.New("device_id is required")
	case task.ProfileICCID == "":
		return errors.New("profile_iccid is required")
	case task.IntervalDays < minIntervalDays || task.IntervalDays > maxIntervalDays:
		return fmt.Errorf("interval_days must be between %d and %d", minIntervalDays, maxIntervalDays)
	case task.RetryCount < 0 || task.RetryCount > maxRetryCount:
		return fmt.Errorf("retry_count must be between 0 and %d", maxRetryCount)
	}
	if err := validateTaskKind(task); err != nil {
		return err
	}
	if task.Environment != EnvironmentVoWiFi && task.Environment != EnvironmentCellular {
		return errors.New("environment must be vowifi or cellular")
	}
	if task.TaskType == TaskTypePublicIP && task.Environment != EnvironmentCellular {
		return errors.New("public_ip tasks require the cellular environment")
	}
	return nil
}

func validateTaskKind(task Task) error {
	switch task.TaskType {
	case TaskTypeSMS:
		if task.Payload.Phone == "" || strings.TrimSpace(task.Payload.Message) == "" {
			return errors.New("SMS tasks require payload.phone and payload.message")
		}
		if !validDialAddress(task.Payload.Phone) {
			return errors.New("payload.phone must contain an optional leading + followed by 2 to 15 digits")
		}
	case TaskTypeCall:
		if task.Payload.Phone == "" {
			return errors.New("call tasks require payload.phone")
		}
		if !validDialAddress(task.Payload.Phone) {
			return errors.New("payload.phone must contain an optional leading + followed by 2 to 15 digits")
		}
		if task.Payload.HoldSeconds < 1 || task.Payload.HoldSeconds > maxCallSeconds {
			return fmt.Errorf("payload.hold_seconds must be between 1 and %d", maxCallSeconds)
		}
	case TaskTypePublicIP:
	default:
		return errors.New("task_type must be sms, call, or public_ip")
	}
	return nil
}

func validDialAddress(value string) bool {
	digits := value
	if strings.HasPrefix(digits, "+") {
		digits = digits[1:]
	}
	if len(digits) < 2 || len(digits) > 15 {
		return false
	}
	for index := range len(digits) {
		if digits[index] < '0' || digits[index] > '9' {
			return false
		}
	}
	return true
}

func CalculateNextRun(task Task, now time.Time) (time.Time, error) {
	location, err := time.LoadLocation(task.Timezone)
	if err != nil {
		return time.Time{}, fmt.Errorf("invalid timezone: %w", err)
	}
	local, err := time.ParseInLocation("2006-01-02 15:04", task.StartDate+" "+task.RunTime, location)
	if err != nil {
		return time.Time{}, fmt.Errorf("invalid start_date or run_time: %w", err)
	}
	localNow := now.In(location)
	for !local.After(localNow) {
		local = local.AddDate(0, 0, task.IntervalDays)
	}
	return local.UTC(), nil
}

func AdvanceNextRun(task Task, now time.Time) (time.Time, error) {
	if task.NextRunAt.IsZero() || task.IntervalDays < minIntervalDays {
		return time.Time{}, errors.New("invalid persisted next run schedule")
	}
	location, err := time.LoadLocation(task.Timezone)
	if err != nil {
		return time.Time{}, fmt.Errorf("invalid timezone: %w", err)
	}
	next := task.NextRunAt.In(location)
	for !next.After(now.In(location)) {
		next = next.AddDate(0, 0, task.IntervalDays)
	}
	return next.UTC(), nil
}

func canonicalICCID(value string) string {
	return strings.TrimRight(strings.Trim(strings.TrimSpace(value), "\""), "Ff")
}
