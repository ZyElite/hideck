package commandcenter

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/iniwex5/vohive/internal/db"
	"github.com/iniwex5/vohive/internal/notify"
	"github.com/iniwex5/vohive/pkg/logger"
)

type Service struct {
	commands *notify.CommandService
	store    Store
	now      func() time.Time

	mu          sync.RWMutex
	subscribers map[uint64]chan Event
	nextSubID   uint64
}

type eventContent struct {
	kind        string
	text        string
	attachments []notify.CommandAttachment
}

type eventPayload struct {
	Attachments []notify.CommandAttachment `json:"attachments,omitempty"`
}

func NewService(commands *notify.CommandService, store Store) *Service {
	return &Service{commands: commands, store: store, now: time.Now, subscribers: make(map[uint64]chan Event)}
}

func (s *Service) Definitions() []notify.CommandDefinition {
	return s.commands.Definitions()
}

func (s *Service) Execute(ctx context.Context, request ExecuteRequest) (db.CommandExecution, error) {
	definition, args, err := s.commands.DefinitionForInput(request.Input)
	if err != nil {
		return db.CommandExecution{}, err
	}
	execution, event, err := s.createExecution(ctx, request.Input, definition.Name, args)
	if err != nil {
		return db.CommandExecution{}, err
	}
	s.publish(event)
	go s.run(execution.ID, definition.Async, request.Input)
	return execution, nil
}

func (s *Service) ListEvents(ctx context.Context, after uint64, limit int) ([]Event, error) {
	return s.store.ListEvents(ctx, after, limit)
}

func (s *Service) ListEventsBefore(ctx context.Context, before uint64, limit int) ([]Event, error) {
	return s.store.ListEventsBefore(ctx, before, limit)
}

func (s *Service) ClearCompleted(ctx context.Context) (int64, error) {
	return s.store.ClearCompleted(ctx)
}

func (s *Service) Subscribe() (<-chan Event, func()) {
	s.mu.Lock()
	s.nextSubID++
	id := s.nextSubID
	ch := make(chan Event, 32)
	s.subscribers[id] = ch
	s.mu.Unlock()
	return ch, func() { s.unsubscribe(id) }
}

func (s *Service) createExecution(ctx context.Context, input, name string, args []string) (db.CommandExecution, Event, error) {
	now := s.now()
	encodedArgs, err := json.Marshal(args)
	if err != nil {
		return db.CommandExecution{}, Event{}, err
	}
	execution := db.CommandExecution{ID: uuid.NewString(), Input: strings.TrimSpace(input), Command: name,
		ArgumentsJSON: string(encodedArgs), State: StateRunning, StartedAt: &now, CreatedAt: now, UpdatedAt: now}
	event := db.CommandEvent{ExecutionID: execution.ID, Kind: EventAccepted, Text: "命令已受理", CreatedAt: now}
	created, err := s.store.Create(ctx, execution, event)
	return execution, created, err
}

func (s *Service) run(id string, async bool, input string) {
	ctx := &replyContext{service: s, executionID: id, finalReply: async}
	result, err := s.commands.Execute(ctx, input)
	if err != nil {
		s.finish(id, EventError, err.Error())
		return
	}
	if async {
		ctx.activate(result)
		return
	}
	s.finish(id, resultKind(result), result)
}

func (s *Service) finish(id, kind, text string) {
	s.finishContent(id, eventContent{kind: kind, text: text})
}

func (s *Service) finishContent(id string, content eventContent) {
	state, message := StateCompleted, ""
	if content.kind == EventError {
		state, message = StateFailed, content.text
	}
	if err := s.store.Finish(context.Background(), id, state, message, s.now()); err != nil {
		logger.Error("命令执行状态持久化失败", "execution_id", id, "err", err)
		return
	}
	s.addEvent(id, content)
}

func (s *Service) addEvent(id string, content eventContent) {
	payload, err := encodeEventPayload(content.attachments)
	if err != nil {
		logger.Error("命令事件附件编码失败", "execution_id", id, "err", err)
		return
	}
	event, err := s.store.AddEvent(context.Background(), db.CommandEvent{
		ExecutionID: id, Kind: content.kind, Text: content.text, PayloadJSON: payload, CreatedAt: s.now(),
	})
	if err == nil {
		s.publish(event)
		return
	}
	logger.Error("命令事件持久化失败", "execution_id", id, "kind", content.kind, "err", err)
}

func encodeEventPayload(attachments []notify.CommandAttachment) (string, error) {
	if len(attachments) == 0 {
		return "", nil
	}
	encoded, err := json.Marshal(eventPayload{Attachments: attachments})
	return string(encoded), err
}

func (s *Service) publish(event Event) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, ch := range s.subscribers {
		select {
		case ch <- event:
		default:
			logger.Warn("命令事件订阅者积压，客户端需按游标重连", "event_id", event.ID)
		}
	}
}

func (s *Service) unsubscribe(id uint64) {
	s.mu.Lock()
	delete(s.subscribers, id)
	s.mu.Unlock()
}

func resultKind(result string) string {
	if strings.Contains(result, "失败") || strings.Contains(result, "参数错误") {
		return EventError
	}
	return EventResult
}

type replyContext struct {
	service     *Service
	executionID string
	finalReply  bool
	once        sync.Once
	mu          sync.Mutex
	active      bool
	pending     eventContent
	hasPending  bool
	progress    []string
}

func (c *replyContext) Reply(text string) {
	c.reply(eventContent{kind: EventProgress, text: text})
}

func (c *replyContext) ReplyWithAttachments(text string, attachments []notify.CommandAttachment) {
	c.reply(eventContent{kind: EventProgress, text: text, attachments: attachments})
}

func (c *replyContext) Progress(text string) {
	if !c.finalReply {
		c.service.addEvent(c.executionID, eventContent{kind: EventProgress, text: text})
		return
	}
	c.mu.Lock()
	if !c.active {
		c.progress = append(c.progress, text)
		c.mu.Unlock()
		return
	}
	c.mu.Unlock()
	c.service.addEvent(c.executionID, eventContent{kind: EventProgress, text: text})
}

func (c *replyContext) reply(content eventContent) {
	if !c.finalReply {
		c.service.addEvent(c.executionID, content)
		return
	}
	c.mu.Lock()
	if !c.active {
		c.pending = content
		c.hasPending = true
		c.mu.Unlock()
		return
	}
	c.mu.Unlock()
	c.finish(content)
}

func (c *replyContext) activate(initial string) {
	if !strings.Contains(initial, "已受理") {
		c.service.finish(c.executionID, resultKind(initial), initial)
		return
	}
	c.service.addEvent(c.executionID, eventContent{kind: EventProgress, text: initial})
	c.mu.Lock()
	c.active = true
	pending := c.pending
	hasPending := c.hasPending
	progress := append([]string(nil), c.progress...)
	c.pending = eventContent{}
	c.hasPending = false
	c.progress = nil
	c.mu.Unlock()
	for _, text := range progress {
		c.service.addEvent(c.executionID, eventContent{kind: EventProgress, text: text})
	}
	if hasPending {
		c.finish(pending)
	}
}

func (c *replyContext) finish(content eventContent) {
	c.once.Do(func() {
		content.kind = resultKind(content.text)
		c.service.finishContent(c.executionID, content)
	})
}
