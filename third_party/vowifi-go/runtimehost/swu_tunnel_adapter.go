package runtimehost

import (
	"context"
	"net"
	"sync"
	"time"

	"github.com/iniwex5/vowifi-go/engine/swu"
	"github.com/iniwex5/vowifi-go/internal/vowifi/epdg"
	"github.com/iniwex5/vowifi-go/internal/vowifi/logging"
)

type swuTunnelAdapter struct {
	*swu.Session
	manager  *epdg.Manager
	deviceID string
	config   *swu.Config
	timeout  time.Duration
	stopOnce sync.Once
}

type swuTunnelAdapterConfig struct {
	Manager              *epdg.Manager
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
	err := adapter.connectOnce(ctx)
	if !epdg.ShouldRetryFreshTunnel(ctx, err) {
		return err
	}
	// First IKE/SOCKS5 UDP Associate often drops the first datagrams.
	// A new session (what the UI reconnect button does) usually succeeds.
	logging.Info("SWu first ePDG wait timed out; retrying with a fresh tunnel",
		"device", adapter.deviceID)
	adapter.stopOnce = sync.Once{}
	return adapter.connectOnce(ctx)
}

func (adapter *swuTunnelAdapter) connectOnce(ctx context.Context) error {
	session, err := adapter.manager.Start(ctx, adapter.deviceID, adapter.config)
	if err != nil {
		return err
	}
	adapter.Session = session
	if _, err := adapter.manager.Wait(ctx, adapter.deviceID, adapter.timeout); err != nil {
		adapter.stop()
		return err
	}
	return nil
}

func (adapter *swuTunnelAdapter) Shutdown() {
	adapter.stop()
}

func (adapter *swuTunnelAdapter) stop() {
	adapter.stopOnce.Do(func() {
		_ = adapter.manager.Stop(adapter.deviceID)
	})
}

func (adapter *swuTunnelAdapter) UpdateAddresses(oldIP, newIP net.IP) error {
	return adapter.Session.UpdateAddresses(oldIP, newIP)
}
