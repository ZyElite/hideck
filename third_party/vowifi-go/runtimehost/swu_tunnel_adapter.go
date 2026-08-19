package runtimehost

import (
	"context"
	"errors"
	"net"
	"sync"
	"time"

	"github.com/iniwex5/vowifi-go/engine/swu"
	"github.com/iniwex5/vowifi-go/internal/vowifi/epdg"
	"github.com/iniwex5/vowifi-go/internal/vowifi/logging"
)

type swuTunnelAdapter struct {
	*swu.Session
	manager       epdgSessionManager
	deviceID      string
	config        *swu.Config
	timeout       time.Duration
	connectMu     sync.Mutex
	lifecycleMu   sync.Mutex
	activeSession *swu.Session
	connectCancel context.CancelFunc
	closed        bool
}

type epdgSessionManager interface {
	Start(context.Context, string, *swu.Config) (*swu.Session, error)
	Wait(context.Context, string, time.Duration) (swu.SessionSnapshot, error)
	Stop(string) error
}

type swuTunnelAdapterConfig struct {
	Manager              epdgSessionManager
	DeviceID             string
	SessionConfig        *swu.Config
	EstablishmentTimeout time.Duration
}

func newSWUTunnelAdapter(config swuTunnelAdapterConfig) *swuTunnelAdapter {
	return &swuTunnelAdapter{
		manager: config.Manager, deviceID: config.DeviceID,
		config: config.SessionConfig, timeout: config.EstablishmentTimeout,
	}
}

func (adapter *swuTunnelAdapter) Connect(ctx context.Context) error {
	adapter.connectMu.Lock()
	defer adapter.connectMu.Unlock()
	if ctx == nil {
		ctx = context.Background()
	}
	connectCtx, cancel := context.WithCancel(ctx)
	if err := adapter.beginConnect(cancel); err != nil {
		cancel()
		return err
	}
	defer adapter.finishConnect(cancel)

	err := adapter.connectOnce(connectCtx)
	if !epdg.ShouldRetryFreshTunnel(connectCtx, err) {
		return err
	}
	// First IKE/SOCKS5 UDP Associate often drops the first datagrams.
	// A new session (what the UI reconnect button does) usually succeeds.
	logging.Info("SWu first ePDG wait timed out; retrying with a fresh tunnel",
		"device", adapter.deviceID)
	return adapter.connectOnce(connectCtx)
}

func (adapter *swuTunnelAdapter) connectOnce(ctx context.Context) error {
	session, err := adapter.startSession(ctx)
	if err != nil {
		return err
	}
	if _, err := adapter.manager.Wait(ctx, adapter.deviceID, adapter.timeout); err != nil {
		adapter.stopSession(session)
		return err
	}
	return nil
}

func (adapter *swuTunnelAdapter) Shutdown() {
	adapter.lifecycleMu.Lock()
	if adapter.closed {
		adapter.lifecycleMu.Unlock()
		return
	}
	adapter.closed = true
	cancel := adapter.connectCancel
	session := adapter.activeSession
	adapter.activeSession = nil
	adapter.lifecycleMu.Unlock()
	if cancel != nil {
		cancel()
	}
	if session != nil {
		_ = adapter.manager.Stop(adapter.deviceID)
	}
}

func (adapter *swuTunnelAdapter) beginConnect(cancel context.CancelFunc) error {
	adapter.lifecycleMu.Lock()
	defer adapter.lifecycleMu.Unlock()
	if adapter.closed {
		return errors.New("runtimehost: SWu tunnel adapter is shut down")
	}
	adapter.connectCancel = cancel
	return nil
}

func (adapter *swuTunnelAdapter) finishConnect(cancel context.CancelFunc) {
	cancel()
	adapter.lifecycleMu.Lock()
	adapter.connectCancel = nil
	adapter.lifecycleMu.Unlock()
}

func (adapter *swuTunnelAdapter) startSession(ctx context.Context) (*swu.Session, error) {
	adapter.lifecycleMu.Lock()
	defer adapter.lifecycleMu.Unlock()
	if adapter.closed {
		return nil, errors.New("runtimehost: SWu tunnel adapter is shut down")
	}
	session, err := adapter.manager.Start(ctx, adapter.deviceID, adapter.config)
	if err != nil {
		return nil, err
	}
	adapter.Session = session
	adapter.activeSession = session
	return session, nil
}

func (adapter *swuTunnelAdapter) stopSession(session *swu.Session) {
	adapter.lifecycleMu.Lock()
	if adapter.activeSession != session {
		adapter.lifecycleMu.Unlock()
		return
	}
	adapter.activeSession = nil
	adapter.lifecycleMu.Unlock()
	_ = adapter.manager.Stop(adapter.deviceID)
}

func (adapter *swuTunnelAdapter) UpdateAddresses(oldIP, newIP net.IP) error {
	return adapter.Session.UpdateAddresses(oldIP, newIP)
}
