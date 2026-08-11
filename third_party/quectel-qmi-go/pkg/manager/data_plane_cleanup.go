package manager

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/iniwex5/quectel-qmi-go/pkg/netcfg"
	"github.com/iniwex5/quectel-qmi-go/pkg/qmi"
)

type dataPlaneSnapshot struct {
	ifname   string
	wdsV4    *qmi.WDSService
	wdsV6    *qmi.WDSService
	handleV4 uint32
	handleV6 uint32
}

func (m *Manager) networkConfig() netcfg.NetworkConfigurator {
	if m.networkConfigurator != nil {
		return m.networkConfigurator
	}
	return netcfg.GetConfigurator()
}

func (m *Manager) dataPlaneInterfaceLocked() string {
	if m.muxIface != "" {
		return m.muxIface
	}
	return m.cfg.Device.NetInterface
}

func (m *Manager) dataPlaneInterface() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.dataPlaneInterfaceLocked()
}

func (m *Manager) takeDataPlaneSnapshot() dataPlaneSnapshot {
	m.mu.Lock()
	defer m.mu.Unlock()
	snapshot := dataPlaneSnapshot{
		ifname:   m.dataPlaneInterfaceLocked(),
		wdsV4:    m.wds,
		wdsV6:    m.wdsV6,
		handleV4: m.handleV4,
		handleV6: m.handleV6,
	}
	m.handleV4 = 0
	m.handleV6 = 0
	m.settings = nil
	m.lastIPCheck = time.Time{}
	return snapshot
}

func (m *Manager) stopNetworkInterface(ctx context.Context, wds *qmi.WDSService, handle uint32) error {
	if m.stopNetworkInterfaceHook != nil {
		return m.stopNetworkInterfaceHook(ctx, wds, handle)
	}
	return wds.StopNetworkInterface(ctx, handle)
}

func (m *Manager) stopDataCall(wds *qmi.WDSService, handle uint32, family string) error {
	if handle == 0 {
		return nil
	}
	if wds == nil {
		return fmt.Errorf("stop %s data call: WDS service missing for handle %#x", family, handle)
	}
	ctx, cancel := m.opContext(m.cfg.Timeouts.Stop)
	defer cancel()
	if err := m.stopNetworkInterface(ctx, wds, handle); err != nil {
		return fmt.Errorf("stop %s data call handle %#x: %w", family, handle, err)
	}
	return nil
}

func (m *Manager) cleanupDataPlane(stopCalls bool) error {
	snapshot := m.takeDataPlaneSnapshot()
	var cleanupErrors []error
	if stopCalls {
		cleanupErrors = append(cleanupErrors,
			m.stopDataCall(snapshot.wdsV4, snapshot.handleV4, "IPv4"),
			m.stopDataCall(snapshot.wdsV6, snapshot.handleV6, "IPv6"),
		)
	}
	if snapshot.ifname == "" {
		return errors.Join(cleanupErrors...)
	}
	network := m.networkConfig()
	cleanupErrors = append(cleanupErrors,
		flushNetworkInterface(network, snapshot.ifname),
		wrapNetworkError("bring down", snapshot.ifname, network.BringDown(snapshot.ifname)),
	)
	return errors.Join(cleanupErrors...)
}

func flushNetworkInterface(network netcfg.NetworkConfigurator, ifname string) error {
	return errors.Join(
		wrapNetworkError("flush routes", ifname, network.FlushRoutes(ifname)),
		wrapNetworkError("flush addresses", ifname, network.FlushAddresses(ifname)),
	)
}

func wrapNetworkError(operation, ifname string, err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%s on %s: %w", operation, ifname, err)
}
