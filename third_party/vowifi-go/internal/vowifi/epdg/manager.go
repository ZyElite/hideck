// Package epdg manages SWu sessions by device identity.
package epdg

import (
	"context"
	"errors"
	"time"

	"github.com/iniwex5/vowifi-go/engine/swu"
	"go.uber.org/zap"
)

const waitPollInterval = 200 * time.Millisecond

// ErrEstablishmentTimeout means IKE/SWu did not come up before Wait expired.
// Callers may start a fresh session (new SOCKS5 associate) once.
var ErrEstablishmentTimeout = errors.New("等待 ePDG 隧道建立超时")

type Manager struct {
	mgr *swu.SessionManager
}

func New() *Manager {
	zap.ReplaceGlobals(zap.L().WithOptions(zap.AddCallerSkip(-1)))
	return &Manager{mgr: swu.NewSessionManager()}
}

func (m *Manager) Start(
	ctx context.Context,
	deviceID string,
	config *swu.Config,
) (*swu.Session, error) {
	return m.mgr.Start(ctx, deviceID, config)
}

func (m *Manager) Stop(deviceID string) error {
	return m.mgr.Stop(deviceID)
}

func (m *Manager) Snapshot(deviceID string) (swu.SessionSnapshot, bool) {
	session, exists := m.mgr.Get(deviceID)
	if !exists || session == nil {
		return swu.SessionSnapshot{}, false
	}
	return session.Snapshot(), true
}

func (m *Manager) Wait(
	ctx context.Context,
	deviceID string,
	timeout time.Duration,
) (swu.SessionSnapshot, error) {
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	ticker := time.NewTicker(waitPollInterval)
	defer ticker.Stop()
	for {
		if snapshot, done, err := m.waitResult(deviceID); done {
			return snapshot, err
		}
		select {
		case <-ticker.C:
		case <-timer.C:
			return swu.SessionSnapshot{}, ErrEstablishmentTimeout
		case <-ctx.Done():
			return swu.SessionSnapshot{}, ctx.Err()
		}
	}
}

// ShouldRetryFreshTunnel reports whether the first SWu wait died of a
// timeout while the caller is still allowed to start again.
func ShouldRetryFreshTunnel(ctx context.Context, err error) bool {
	return err != nil && ctx != nil && ctx.Err() == nil && errors.Is(err, ErrEstablishmentTimeout)
}

func (m *Manager) waitResult(deviceID string) (swu.SessionSnapshot, bool, error) {
	snapshot, exists := m.Snapshot(deviceID)
	if !exists {
		return swu.SessionSnapshot{}, false, nil
	}
	if snapshot.Established {
		return snapshot, true, nil
	}
	if snapshot.LastError != "" {
		return swu.SessionSnapshot{}, true, errors.New("ePDG 会话失败: " + snapshot.LastError)
	}
	return swu.SessionSnapshot{}, false, nil
}
