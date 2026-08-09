package swu

import (
	"context"
	"errors"
	"sync"

	"github.com/iniwex5/vowifi-go/engine/logger"
	"go.uber.org/zap"
)

// SessionManager owns sessions and cancellation functions by device ID.
type SessionManager struct {
	mu       sync.Mutex
	sessions map[string]*Session
	cancels  map[string]context.CancelFunc
}

func NewSessionManager() *SessionManager {
	return &SessionManager{
		sessions: make(map[string]*Session),
		cancels:  make(map[string]context.CancelFunc),
	}
}

// Start restores the original context/config session constructor.
func (m *SessionManager) Start(
	ctx context.Context,
	deviceID string,
	config *Config,
) (*Session, error) {
	if deviceID == "" {
		return nil, errors.New("session id 不能为空")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	session := NewSession(config, logger.With(zap.String("device", deviceID)))
	sessionContext, cancel := context.WithCancel(ctx)
	m.mu.Lock()
	if _, exists := m.sessions[deviceID]; exists {
		m.mu.Unlock()
		cancel()
		return nil, errors.New("session id 已存在")
	}
	m.sessions[deviceID], m.cancels[deviceID] = session, cancel
	m.mu.Unlock()
	go m.run(sessionContext, session)
	return session, nil
}

func (m *SessionManager) run(ctx context.Context, session *Session) {
	if err := session.Connect(ctx); err != nil {
		if !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
			session.Logger.Error("SWu managed session exited", zap.Error(err))
		}
		return
	}
	select {
	case <-ctx.Done():
		session.Shutdown()
	case <-session.done:
	}
}

// Stop removes and closes the original managed session.
func (m *SessionManager) Stop(deviceID string) error {
	m.mu.Lock()
	session, exists := m.sessions[deviceID]
	cancel := m.cancels[deviceID]
	if exists {
		delete(m.sessions, deviceID)
		delete(m.cancels, deviceID)
	}
	m.mu.Unlock()
	if !exists {
		return errors.New("session id 不存在")
	}
	if cancel != nil {
		cancel()
	}
	if session != nil {
		session.Shutdown()
	}
	return nil
}

// Get restores the original session-plus-presence result.
func (m *SessionManager) Get(deviceID string) (*Session, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	session, exists := m.sessions[deviceID]
	return session, exists
}

// Register retains the additive direct-session registry behavior.
func (m *SessionManager) Register(deviceID string, session *Session) {
	m.mu.Lock()
	m.sessions[deviceID] = session
	m.mu.Unlock()
}

// Lookup retains the additive nil-on-missing lookup behavior.
func (m *SessionManager) Lookup(deviceID string) *Session {
	session, _ := m.Get(deviceID)
	return session
}

// Unregister retains the additive remove-and-shutdown behavior.
func (m *SessionManager) Unregister(deviceID string) {
	m.mu.Lock()
	session, exists := m.sessions[deviceID]
	if exists {
		delete(m.sessions, deviceID)
		delete(m.cancels, deviceID)
	}
	m.mu.Unlock()
	if exists && session != nil {
		session.Shutdown()
	}
}
