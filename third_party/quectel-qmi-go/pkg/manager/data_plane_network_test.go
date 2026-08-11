package manager

import (
	"context"
	"errors"
	"net"
	"slices"
	"strconv"
	"testing"
	"time"

	"github.com/iniwex5/quectel-qmi-go/pkg/qmi"
)

type networkCall struct {
	operation string
	ifname    string
	value     string
}

type recordingNetworkConfigurator struct {
	calls    []networkCall
	failures map[string]error
	muxIface string
}

func (r *recordingNetworkConfigurator) record(operation, ifname, value string) error {
	r.calls = append(r.calls, networkCall{operation: operation, ifname: ifname, value: value})
	return r.failures[operation+":"+ifname]
}

func (r *recordingNetworkConfigurator) SetIPAddress(ifname string, ip net.IP, prefixLen int) error {
	return r.record("set-v4", ifname, ip.String()+"/"+strconv.Itoa(prefixLen))
}

func (r *recordingNetworkConfigurator) SetIPv6Address(ifname string, ip net.IP, prefixLen int) error {
	return r.record("set-v6", ifname, ip.String()+"/"+strconv.Itoa(prefixLen))
}

func (r *recordingNetworkConfigurator) FlushAddresses(ifname string) error {
	return r.record("flush-addresses", ifname, "")
}

func (r *recordingNetworkConfigurator) AddDefaultRoute(ifname string, gateway net.IP) error {
	return r.record("add-route", ifname, gateway.String())
}

func (r *recordingNetworkConfigurator) AddDefaultRouteDirect(ifname string, ipv6 bool) error {
	family := "v4"
	if ipv6 {
		family = "v6"
	}
	return r.record("add-direct-route", ifname, family)
}

func (r *recordingNetworkConfigurator) FlushRoutes(ifname string) error {
	return r.record("flush-routes", ifname, "")
}

func (r *recordingNetworkConfigurator) BringUp(ifname string) error {
	return r.record("up", ifname, "")
}

func (r *recordingNetworkConfigurator) BringDown(ifname string) error {
	return r.record("down", ifname, "")
}

func (r *recordingNetworkConfigurator) SetMTU(ifname string, mtu int) error {
	return r.record("mtu", ifname, strconv.Itoa(mtu))
}

func (r *recordingNetworkConfigurator) GetCurrentIP(string) (net.IP, error) { return nil, nil }
func (r *recordingNetworkConfigurator) IsUp(string) (bool, error)           { return true, nil }

func (r *recordingNetworkConfigurator) UpdateDNS(dns1, dns2 string) error {
	return r.record("dns", "", dns1+","+dns2)
}

func (r *recordingNetworkConfigurator) RestoreDNS() error { return nil }

func (r *recordingNetworkConfigurator) AddQMAPMux(masterIface string, muxID uint8) (string, error) {
	return r.muxIface, r.record("add-mux", masterIface, strconv.Itoa(int(muxID)))
}

func (r *recordingNetworkConfigurator) DelQMAPMux(masterIface string, muxID uint8) error {
	return r.record("del-mux", masterIface, strconv.Itoa(int(muxID)))
}

func (r *recordingNetworkConfigurator) GetQMAPMuxIface(string, uint8) string { return r.muxIface }
func (r *recordingNetworkConfigurator) EnableRawIP(ifname string) error {
	return r.record("raw-ip", ifname, "")
}

func newDataPlaneTestManager(network *recordingNetworkConfigurator) *Manager {
	m := newRecoveryTestManager()
	m.cfg = normalizeConfig(Config{Device: ModemDevice{NetInterface: "wwan0"}, NoDNS: true})
	m.networkConfigurator = network
	m.wds = &qmi.WDSService{}
	m.wdsV6 = &qmi.WDSService{}
	return m
}

func TestQMAPDisconnectCleansMuxAndBothHandles(t *testing.T) {
	network := &recordingNetworkConfigurator{}
	m := newDataPlaneTestManager(network)
	m.muxIface = "qmimux0"
	m.handleV4 = 0x11
	m.handleV6 = 0x22
	m.settings = &qmi.RuntimeSettings{IPv6Address: net.ParseIP("2001:db8::1")}
	m.state = StateConnected

	var stopped []uint32
	m.stopNetworkInterfaceHook = func(_ context.Context, _ *qmi.WDSService, handle uint32) error {
		stopped = append(stopped, handle)
		return nil
	}

	if err := m.doDisconnect(); err != nil {
		t.Fatalf("doDisconnect() error = %v", err)
	}
	if !slices.Equal(stopped, []uint32{0x11, 0x22}) {
		t.Fatalf("stopped handles = %#v, want IPv4 and IPv6 handles", stopped)
	}
	assertOnlyInterfaceForOperations(t, network.calls, "qmimux0", "flush-routes", "flush-addresses", "down")
	if m.handleV4 != 0 || m.handleV6 != 0 || m.settings != nil {
		t.Fatalf("data-plane cache not cleared: handles=(%#x,%#x) settings=%+v", m.handleV4, m.handleV6, m.settings)
	}
}

func TestCarrierLossUsesQMAPCleanupAndReconnects(t *testing.T) {
	network := &recordingNetworkConfigurator{}
	m := newDataPlaneTestManager(network)
	m.client = &qmi.Client{}
	m.muxIface = "qmimux0"
	m.handleV4 = 0x11
	m.handleV6 = 0x22
	m.settings = &qmi.RuntimeSettings{IPv6Address: net.ParseIP("2001:db8::1")}
	m.state = StateConnected
	m.desiredConnection = true
	m.cfg.AutoReconnect = true
	m.queryPacketServiceState = func(context.Context) (qmi.ConnectionStatus, error) {
		return qmi.StatusDisconnected, nil
	}
	m.stopNetworkInterfaceHook = func(context.Context, *qmi.WDSService, uint32) error {
		t.Fatal("carrier-loss cleanup must not stop an already-lost data call")
		return nil
	}

	m.doStatusCheck(false)

	assertOnlyInterfaceForOperations(t, network.calls, "qmimux0", "flush-routes", "flush-addresses", "down")
	if m.handleV4 != 0 || m.handleV6 != 0 || m.settings != nil {
		t.Fatal("carrier-loss cleanup left stale handles or settings")
	}
	select {
	case event := <-m.eventCh:
		if event != eventStart {
			t.Fatalf("reconnect event = %v, want eventStart", event)
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("carrier loss did not schedule reconnect")
	}
}

func TestQMAPReconnectAppliesFreshIPv6Settings(t *testing.T) {
	network := &recordingNetworkConfigurator{}
	m := newDataPlaneTestManager(network)
	m.muxIface = "qmimux0"
	m.handleV4 = 0x11
	m.handleV6 = 0x22
	currentIPv6 := "2001:db8:1::10"
	m.getRuntimeSettingsHook = runtimeSettingsHook(&currentIPv6)

	if err := m.configureNetwork(); err != nil {
		t.Fatalf("first configureNetwork() error = %v", err)
	}
	if got := m.Settings().IPv6Address.String(); got != currentIPv6 {
		t.Fatalf("first cached IPv6 = %s, want %s", got, currentIPv6)
	}
	if err := m.cleanupDataPlane(false); err != nil {
		t.Fatalf("cleanupDataPlane() error = %v", err)
	}

	currentIPv6 = "2001:db8:2::20"
	m.handleV4 = 0x33
	m.handleV6 = 0x44
	if err := m.configureNetwork(); err != nil {
		t.Fatalf("second configureNetwork() error = %v", err)
	}
	if got := m.Settings().IPv6Address.String(); got != currentIPv6 {
		t.Fatalf("reconnected cached IPv6 = %s, want fresh %s", got, currentIPv6)
	}
	assertCallPresent(t, network.calls, networkCall{operation: "set-v6", ifname: "qmimux0", value: currentIPv6 + "/64"})
	assertCallPresent(t, network.calls, networkCall{operation: "add-route", ifname: "qmimux0", value: "2001:db8:2::1"})
	assertNoOperationOnInterface(t, network.calls, "wwan0", "flush-routes", "flush-addresses", "down", "set-v4", "set-v6", "add-route")
}

func TestRegularQMIDisconnectCleansPhysicalInterface(t *testing.T) {
	network := &recordingNetworkConfigurator{}
	m := newDataPlaneTestManager(network)
	m.wdsV6 = nil
	m.handleV4 = 0x11
	m.stopNetworkInterfaceHook = func(context.Context, *qmi.WDSService, uint32) error { return nil }

	if err := m.cleanupDataPlane(true); err != nil {
		t.Fatalf("cleanupDataPlane() error = %v", err)
	}
	assertOnlyInterfaceForOperations(t, network.calls, "wwan0", "flush-routes", "flush-addresses", "down")
}

func TestDisconnectSurfacesCleanupFailureAfterClearingCache(t *testing.T) {
	wantErr := errors.New("route cleanup failed")
	network := &recordingNetworkConfigurator{failures: map[string]error{"flush-routes:qmimux0": wantErr}}
	m := newDataPlaneTestManager(network)
	m.muxIface = "qmimux0"
	m.handleV4 = 0x11
	m.stopNetworkInterfaceHook = func(context.Context, *qmi.WDSService, uint32) error { return nil }

	err := m.cleanupDataPlane(true)
	if !errors.Is(err, wantErr) {
		t.Fatalf("cleanupDataPlane() error = %v, want wrapped %v", err, wantErr)
	}
	if m.handleV4 != 0 || m.settings != nil {
		t.Fatal("cleanup failure left stale cached state")
	}
}

func runtimeSettingsHook(currentIPv6 *string) func(context.Context, *qmi.WDSService, uint8) (*qmi.RuntimeSettings, error) {
	return func(_ context.Context, _ *qmi.WDSService, family uint8) (*qmi.RuntimeSettings, error) {
		if family == qmi.IpFamilyV4 {
			return &qmi.RuntimeSettings{
				IPv4Address: net.ParseIP("10.0.0.2"),
				IPv4Subnet:  net.CIDRMask(30, 32),
				IPv4Gateway: net.ParseIP("10.0.0.1"),
				MTU:         1500,
			}, nil
		}
		return &qmi.RuntimeSettings{
			IPv6Address: net.ParseIP(*currentIPv6),
			IPv6Prefix:  64,
			IPv6Gateway: net.ParseIP("2001:db8:2::1"),
		}, nil
	}
}

func assertOnlyInterfaceForOperations(t *testing.T, calls []networkCall, want string, operations ...string) {
	t.Helper()
	for _, call := range calls {
		if slices.Contains(operations, call.operation) && call.ifname != want {
			t.Fatalf("%s operated on %s, want only %s; calls=%+v", call.operation, call.ifname, want, calls)
		}
	}
	for _, operation := range operations {
		assertCallPresent(t, calls, networkCall{operation: operation, ifname: want})
	}
}

func assertCallPresent(t *testing.T, calls []networkCall, want networkCall) {
	t.Helper()
	for _, call := range calls {
		if call.operation == want.operation && call.ifname == want.ifname && (want.value == "" || call.value == want.value) {
			return
		}
	}
	t.Fatalf("missing network call %+v; calls=%+v", want, calls)
}

func assertNoOperationOnInterface(t *testing.T, calls []networkCall, ifname string, operations ...string) {
	t.Helper()
	for _, call := range calls {
		if call.ifname == ifname && slices.Contains(operations, call.operation) {
			t.Fatalf("unexpected %s on %s; calls=%+v", call.operation, ifname, calls)
		}
	}
}
