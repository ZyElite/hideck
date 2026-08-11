package commandcenter

import (
	"context"
	"errors"
	"time"

	"github.com/iniwex5/vohive/internal/db"
)

const (
	StateRunning   = "running"
	StateCompleted = "completed"
	StateFailed    = "failed"

	EventAccepted = "accepted"
	EventProgress = "progress"
	EventResult   = "result"
	EventError    = "error"
)

var ErrUnavailable = errors.New("命令中心未配置")

type ExecuteRequest struct {
	Input string
}

type Event struct {
	ID          uint64               `json:"id"`
	ExecutionID string               `json:"execution_id"`
	Kind        string               `json:"kind"`
	Text        string               `json:"text"`
	Execution   *db.CommandExecution `json:"execution,omitempty"`
	CreatedAt   time.Time            `json:"created_at"`
}

type Store interface {
	Create(context.Context, db.CommandExecution, db.CommandEvent) (Event, error)
	AddEvent(context.Context, db.CommandEvent) (Event, error)
	Finish(context.Context, string, string, string, time.Time) error
	ListEvents(context.Context, uint64, int) ([]Event, error)
	ClearCompleted(context.Context) (int64, error)
}
