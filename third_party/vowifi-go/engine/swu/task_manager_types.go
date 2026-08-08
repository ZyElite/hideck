package swu

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/iniwex5/vowifi-go/engine/ikev2"
)

const taskManagerPollInterval = 500 * time.Millisecond

var (
	// ErrWindowTimeout is the legacy error returned after all IKE retries fail.
	ErrWindowTimeout = errors.New("timeout reached max retries")
	// ErrTaskTimeout retains the interim API name for the same condition.
	ErrTaskTimeout = ErrWindowTimeout
	// ErrTaskManagerStopped is reported by the additive result-channel API.
	ErrTaskManagerStopped = errors.New("ike: task manager stopped")
	// ErrDuplicateMessageID prevents one transaction from orphaning another.
	ErrDuplicateMessageID = errors.New("ike: duplicate request message ID")
)

// RetryConfig is the original IKE sliding-window retransmission policy.
type RetryConfig struct {
	MaxRetries     int
	InitialTimeout time.Duration
	MaxTimeout     time.Duration
	BackoffFactor  float64
}

// DefaultRetryConfig returns the original {5, 4s, unlimited, 1.8x} policy.
func DefaultRetryConfig() *RetryConfig {
	return &RetryConfig{
		MaxRetries: 5, InitialTimeout: 4 * time.Second, BackoffFactor: 1.8,
	}
}

// RetransmitConfig retains the additive configuration used by current callers.
type RetransmitConfig struct {
	MaxRetries   int
	InitialDelay time.Duration
	Backoff      float64
	PollInterval time.Duration
}

var DefaultRetransmitConfig = RetransmitConfig{
	MaxRetries: 5, InitialDelay: 4 * time.Second, Backoff: 1.8,
	PollInterval: taskManagerPollInterval,
}

// OutgoingMessage is one complete IKE transaction. Packets contains every SKF
// fragment and is retransmitted as a unit.
type OutgoingMessage struct {
	MsgID    uint32
	Payloads []ikev2.Payload
	Exchange ikev2.ExchangeType
	Packets  [][]byte

	RetryCount  int
	MaxRetries  int
	NextTimeout time.Duration
	Deadline    time.Time

	CompletionCh chan []byte
	isClosed     bool

	resultCh    chan TaskResponse
	lastSendErr error
	completed   bool
}

// TaskResponse exposes timeout, stop and send failures for additive callers.
type TaskResponse struct {
	Message []byte
	Err     error
}

// TaskManager owns the IKE request window and retransmission lifecycle.
type TaskManager struct {
	ctx    context.Context
	cancel context.CancelFunc
	config *RetryConfig
	device string

	windowSize int
	pending    map[uint32]*OutgoingMessage
	queue      []*OutgoingMessage

	mu       sync.Mutex
	wakeupCh chan struct{}
	sendFunc func([][]byte) error
	sendRaw  func(uint32, []byte) error
	stopped  bool

	checkInterval time.Duration
	stopOnce      sync.Once
	done          chan struct{}
}

// NewTaskManager restores the original constructor and public call shape.
func NewTaskManager(
	ctx context.Context,
	device string,
	config *RetryConfig,
	windowSize int,
	sendFunc func([][]byte) error,
) *TaskManager {
	return newTaskManager(ctx, device, config, windowSize, sendFunc, nil, taskManagerPollInterval)
}

// NewRawTaskManager retains the interim raw-message/result-channel behavior.
func NewRawTaskManager(
	ctx context.Context,
	send func(uint32, []byte) error,
	config *RetransmitConfig,
	windowSize int,
) *TaskManager {
	legacy, interval := convertRetransmitConfig(config)
	return newTaskManager(ctx, "", legacy, windowSize, nil, send, interval)
}

func convertRetransmitConfig(config *RetransmitConfig) (*RetryConfig, time.Duration) {
	if config == nil {
		value := DefaultRetransmitConfig
		config = &value
	}
	interval := config.PollInterval
	if interval <= 0 {
		interval = taskManagerPollInterval
	}
	return &RetryConfig{
		MaxRetries: config.MaxRetries, InitialTimeout: config.InitialDelay,
		BackoffFactor: config.Backoff,
	}, interval
}

func newTaskManager(
	parent context.Context,
	device string,
	config *RetryConfig,
	windowSize int,
	sendFunc func([][]byte) error,
	sendRaw func(uint32, []byte) error,
	checkInterval time.Duration,
) *TaskManager {
	if parent == nil {
		parent = context.Background()
	}
	if config == nil {
		config = DefaultRetryConfig()
	}
	configCopy := *config
	if windowSize < 1 {
		windowSize = 1
	}
	ctx, cancel := context.WithCancel(parent)
	tm := &TaskManager{
		ctx: ctx, cancel: cancel, config: &configCopy, device: device,
		windowSize: windowSize, pending: make(map[uint32]*OutgoingMessage),
		queue: make([]*OutgoingMessage, 0), wakeupCh: make(chan struct{}, 1),
		sendFunc: sendFunc, sendRaw: sendRaw, checkInterval: checkInterval,
		done: make(chan struct{}),
	}
	go tm.windowLoop()
	return tm
}
