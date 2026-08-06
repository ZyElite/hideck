package swu

import (
	"sync"
)

// SessionManager owns a set of Sessions keyed by device ID.
type SessionManager struct {
	mu       sync.RWMutex
	sessions map[string]*Session
}

// NewSessionManager creates an empty session manager.
func NewSessionManager() *SessionManager {
	return &SessionManager{sessions: make(map[string]*Session)}
}

// Get returns the session for a device ID, or nil.
func (m *SessionManager) Get(deviceID string) *Session {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.sessions[deviceID]
}

// Start registers a session under a device ID.
func (m *SessionManager) Start(deviceID string, s *Session) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sessions[deviceID] = s
}

// Stop removes and shuts down the session for a device ID.
func (m *SessionManager) Stop(deviceID string) {
	m.mu.Lock()
	s, ok := m.sessions[deviceID]
	if ok {
		delete(m.sessions, deviceID)
	}
	m.mu.Unlock()
	if ok && s != nil {
		s.Shutdown()
	}
}
