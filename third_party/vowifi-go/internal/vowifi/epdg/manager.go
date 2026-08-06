// Package epdg manages ePDG selection for the SWu tunnel.
//
// Reconstructed from the decompiled internal/vowifi/epdg.
package epdg

import (
	"context"
	"sync"
)

// defaultEPDGFQDN is the default ePDG FQDN (AT&T).
const defaultEPDGFQDN = "epdg.epc.att.net"

// Manager selects and tracks the ePDG endpoint.
type Manager struct {
	mu      sync.RWMutex
	ctx     context.Context
	cancel  context.CancelFunc
	started bool
	addr    string
}

// New creates an ePDG manager with the given address ("" for the default).
func New(addr string) *Manager {
	if addr == "" {
		addr = defaultEPDGFQDN
	}
	ctx, cancel := context.WithCancel(context.Background())
	return &Manager{ctx: ctx, cancel: cancel, addr: addr}
}

// Start begins the manager.
func (m *Manager) Start() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.started {
		return nil
	}
	m.started = true
	return nil
}

// Stop stops the manager.
func (m *Manager) Stop() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.started {
		return
	}
	m.started = false
	m.cancel()
}

// Wait blocks until the manager is stopped.
func (m *Manager) Wait() {
	<-m.ctx.Done()
}

// Snapshot returns the current ePDG address.
func (m *Manager) Snapshot() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.addr
}
