package manager

import (
	"context"
	"fmt"
	"net"
	"time"

	"github.com/iniwex5/quectel-qmi-go/pkg/qmi"
)

type runtimeSettingsPair struct {
	v4     *qmi.RuntimeSettings
	v6     *qmi.RuntimeSettings
	merged *qmi.RuntimeSettings
}

func (m *Manager) getRuntimeSettings(ctx context.Context, wds *qmi.WDSService, family uint8) (*qmi.RuntimeSettings, error) {
	if m.getRuntimeSettingsHook != nil {
		return m.getRuntimeSettingsHook(ctx, wds, family)
	}
	return wds.GetRuntimeSettings(ctx, family)
}

func (m *Manager) loadRuntimeSettings() (*runtimeSettingsPair, error) {
	m.mu.RLock()
	snapshot := dataPlaneSnapshot{wdsV4: m.wds, wdsV6: m.wdsV6, handleV4: m.handleV4, handleV6: m.handleV6}
	m.mu.RUnlock()
	if snapshot.handleV4 == 0 && snapshot.handleV6 == 0 {
		return nil, fmt.Errorf("cannot configure network without an active data call")
	}
	pair := &runtimeSettingsPair{}
	var err error
	if pair.v4, err = m.loadFamilySettings(snapshot.wdsV4, snapshot.handleV4, qmi.IpFamilyV4, "IPv4"); err != nil {
		return nil, err
	}
	if pair.v6, err = m.loadFamilySettings(snapshot.wdsV6, snapshot.handleV6, qmi.IpFamilyV6, "IPv6"); err != nil {
		return nil, err
	}
	pair.merged = mergeRuntimeSettings(pair.v4, pair.v6)
	return pair, nil
}

func (m *Manager) loadFamilySettings(wds *qmi.WDSService, handle uint32, family uint8, name string) (*qmi.RuntimeSettings, error) {
	if handle == 0 {
		return nil, nil
	}
	if wds == nil {
		return nil, fmt.Errorf("query %s settings: WDS service missing for handle %#x", name, handle)
	}
	ctx, cancel := m.opContext(m.cfg.Timeouts.StatusCheck)
	defer cancel()
	settings, err := m.getRuntimeSettings(ctx, wds, family)
	if err != nil {
		return nil, fmt.Errorf("query %s runtime settings: %w", name, err)
	}
	if settings == nil {
		return nil, fmt.Errorf("query %s runtime settings: empty response", name)
	}
	return cloneRuntimeSettings(settings), nil
}

func mergeRuntimeSettings(v4, v6 *qmi.RuntimeSettings) *qmi.RuntimeSettings {
	merged := cloneRuntimeSettings(v4)
	if merged == nil {
		merged = &qmi.RuntimeSettings{}
	}
	if v6 == nil {
		return merged
	}
	merged.IPv6Address = cloneIP(v6.IPv6Address)
	merged.IPv6Prefix = v6.IPv6Prefix
	merged.IPv6Gateway = cloneIP(v6.IPv6Gateway)
	merged.IPv6DNS1 = cloneIP(v6.IPv6DNS1)
	merged.IPv6DNS2 = cloneIP(v6.IPv6DNS2)
	if merged.MTU == 0 {
		merged.MTU = v6.MTU
	}
	return merged
}

func (m *Manager) configureNetwork() error {
	pair, err := m.loadRuntimeSettings()
	if err != nil {
		return err
	}
	ifname := m.dataPlaneInterface()
	if err := m.prepareNetworkInterface(ifname); err != nil {
		return err
	}
	if err := m.applyIPv4Settings(ifname, pair.v4); err != nil {
		return err
	}
	if err := m.applyIPv6Settings(ifname, pair.v6); err != nil {
		return err
	}
	if err := m.applySharedNetworkSettings(ifname, pair.merged); err != nil {
		return err
	}
	m.mu.Lock()
	m.settings = cloneRuntimeSettings(pair.merged)
	m.lastIPCheck = time.Now()
	m.mu.Unlock()
	m.log.WithField("interface", ifname).Info("Network configuration completed")
	return nil
}

func (m *Manager) prepareNetworkInterface(ifname string) error {
	if ifname == "" {
		return fmt.Errorf("network interface is empty")
	}
	network := m.networkConfig()
	master := m.cfg.Device.NetInterface
	if ifname != master {
		if err := network.BringUp(master); err != nil {
			return wrapNetworkError("bring up QMAP master", master, err)
		}
	}
	if err := flushNetworkInterface(network, ifname); err != nil {
		return fmt.Errorf("clear stale network configuration on %s: %w", ifname, err)
	}
	if err := network.BringUp(ifname); err != nil {
		return wrapNetworkError("bring up", ifname, err)
	}
	return nil
}

func (m *Manager) applyIPv4Settings(ifname string, settings *qmi.RuntimeSettings) error {
	if settings == nil {
		return nil
	}
	if settings.IPv4Address == nil {
		return fmt.Errorf("IPv4 runtime settings missing address")
	}
	prefix, bits := settings.IPv4Subnet.Size()
	if bits != net.IPv4len*8 || prefix == 0 {
		prefix = 32
	}
	network := m.networkConfig()
	if err := network.SetIPAddress(ifname, settings.IPv4Address, prefix); err != nil {
		return wrapNetworkError("set IPv4 address", ifname, err)
	}
	if m.cfg.NoRoute {
		return nil
	}
	if settings.IPv4Gateway != nil && !settings.IPv4Gateway.Equal(net.IPv4zero) {
		return wrapNetworkError("add IPv4 default route", ifname, network.AddDefaultRoute(ifname, settings.IPv4Gateway))
	}
	return wrapNetworkError("add direct IPv4 default route", ifname, network.AddDefaultRouteDirect(ifname, false))
}

func (m *Manager) applyIPv6Settings(ifname string, settings *qmi.RuntimeSettings) error {
	if settings == nil {
		return nil
	}
	if settings.IPv6Address == nil {
		return fmt.Errorf("IPv6 runtime settings missing address")
	}
	if settings.IPv6Prefix <= 0 || settings.IPv6Prefix > net.IPv6len*8 {
		return fmt.Errorf("IPv6 runtime settings contain invalid prefix %d", settings.IPv6Prefix)
	}
	network := m.networkConfig()
	if err := network.SetIPv6Address(ifname, settings.IPv6Address, settings.IPv6Prefix); err != nil {
		return wrapNetworkError("set IPv6 address", ifname, err)
	}
	if m.cfg.NoRoute {
		return nil
	}
	if settings.IPv6Gateway != nil && !settings.IPv6Gateway.IsUnspecified() {
		return wrapNetworkError("add IPv6 default route", ifname, network.AddDefaultRoute(ifname, settings.IPv6Gateway))
	}
	return wrapNetworkError("add direct IPv6 default route", ifname, network.AddDefaultRouteDirect(ifname, true))
}

func (m *Manager) applySharedNetworkSettings(ifname string, settings *qmi.RuntimeSettings) error {
	network := m.networkConfig()
	if settings.MTU > 0 {
		if err := network.SetMTU(ifname, settings.MTU); err != nil {
			return wrapNetworkError("set MTU", ifname, err)
		}
	}
	if m.cfg.NoDNS {
		return nil
	}
	dns1, dns2 := preferredDNS(settings)
	if dns1 == "" {
		return nil
	}
	if err := network.UpdateDNS(dns1, dns2); err != nil {
		return fmt.Errorf("update DNS: %w", err)
	}
	return nil
}

func preferredDNS(settings *qmi.RuntimeSettings) (string, string) {
	if settings.IPv4DNS1 != nil {
		return settings.IPv4DNS1.String(), ipString(settings.IPv4DNS2)
	}
	return ipString(settings.IPv6DNS1), ipString(settings.IPv6DNS2)
}

func ipString(ip net.IP) string {
	if ip == nil {
		return ""
	}
	return ip.String()
}
