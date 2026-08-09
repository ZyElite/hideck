package swu

import (
	"context"
	"errors"
	"fmt"
	"net"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/iniwex5/vowifi-go/engine/driver"
)

var _ NetTools = (*driver.NetTools)(nil)
var _ TUN = (*restoredTUN)(nil)

func TestConfiguredDataplaneModeRestoresEnableDriver(t *testing.T) {
	tests := []struct {
		name string
		cfg  *Config
		want string
	}{
		{name: "default", cfg: &Config{}, want: DataplaneModeUserspace},
		{name: "legacy driver switch", cfg: &Config{EnableDriver: true}, want: DataplaneModeTUN},
		{name: "explicit mode wins", cfg: &Config{EnableDriver: true, DataplaneMode: DataplaneModeXFRMI}, want: DataplaneModeXFRMI},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := configuredDataplaneMode(test.cfg)
			if err != nil || got != test.want {
				t.Fatalf("configuredDataplaneMode() = %q, %v, want %q", got, err, test.want)
			}
		})
	}
}

func TestLegacyInjectedTUNAndNetworkLifecycle(t *testing.T) {
	network := &restoredNetTools{}
	tun := &restoredTUN{name: "swu-test"}
	requestedName := ""
	session := NewSession(&Config{
		EnableDriver: true,
		TUNName:      tun.name,
		NetTools:     network,
		TUNFactory: func(name string) (TUN, error) {
			requestedName = name
			return tun, nil
		},
	})
	session.innerIP = net.ParseIP("10.0.0.2")
	session.innerPrefix = 32
	session.pcscfServers = []net.IP{net.ParseIP("192.0.2.8")}

	if err := session.setupTUNDataPlane(); err != nil {
		t.Fatalf("setupTUNDataPlane: %v", err)
	}
	if requestedName != tun.name {
		t.Fatalf("TUN factory name = %q, want %q", requestedName, tun.name)
	}
	wantSetup := []string{
		"up:swu-test", "mtu:swu-test:1358",
		"addr:swu-test:10.0.0.2/32", "route:192.0.2.8/32::swu-test",
	}
	if got := network.operations(); !reflect.DeepEqual(got, wantSetup) {
		t.Fatalf("setup operations = %v, want %v", got, wantSetup)
	}
	if err := session.stopDataPlane(); err != nil {
		t.Fatalf("stopDataPlane: %v", err)
	}
	wantAll := append(wantSetup,
		"del-route:192.0.2.8/32::swu-test",
		"del-addr:swu-test:10.0.0.2/32", "down:swu-test",
	)
	if got := network.operations(); !reflect.DeepEqual(got, wantAll) {
		t.Fatalf("lifecycle operations = %v, want %v", got, wantAll)
	}
	if tun.closeCount() != 1 {
		t.Fatalf("TUN close count = %d, want 1", tun.closeCount())
	}
}

func TestLegacyInjectedNetworkFailureRollsBackAndClosesTUN(t *testing.T) {
	sentinel := errors.New("route rejected")
	network := &restoredNetTools{failPrefix: "route:", failErr: sentinel}
	tun := &restoredTUN{name: "swu-fail"}
	session := NewSession(&Config{
		EnableDriver: true,
		NetTools:     network,
		TUNFactory:   func(string) (TUN, error) { return tun, nil },
	})
	session.innerIP = net.ParseIP("10.0.0.3")
	session.pcscfServers = []net.IP{net.ParseIP("192.0.2.9")}

	err := session.setupTUNDataPlane()
	if !errors.Is(err, sentinel) {
		t.Fatalf("setup error = %v, want %v", err, sentinel)
	}
	operations := network.operations()
	if !containsInOrder(operations, []string{
		"addr:swu-fail:10.0.0.3/32", "route:192.0.2.9/32::swu-fail",
		"del-addr:swu-fail:10.0.0.3/32", "down:swu-fail",
	}) {
		t.Fatalf("rollback operations = %v", operations)
	}
	if tun.closeCount() != 1 || session.tun != nil {
		t.Fatalf("failed TUN cleanup: closes=%d active=%v", tun.closeCount(), session.tun != nil)
	}
}

func TestTUNFactoryCannotReturnNilDevice(t *testing.T) {
	session := NewSession(&Config{
		EnableDriver: true,
		TUNFactory:   func(string) (TUN, error) { return nil, nil },
	})
	if err := session.setupTUNDataPlane(); err == nil || !strings.Contains(err.Error(), "returned nil") {
		t.Fatalf("nil TUN error = %v", err)
	}
}

func TestTimersStartXFRMExpireLifecycle(t *testing.T) {
	plane := &restoredXFRMPlane{outboundSPI: 0x1111, inboundSPI: 0x2222}
	session := NewSession(&Config{})
	session.kernelDataPlane = plane
	rekeyed := make(chan struct{}, 1)
	down := make(chan struct{}, 1)
	session.xfrmRekey = func() error {
		rekeyed <- struct{}{}
		return nil
	}
	session.OnSessionDown = func() { down <- struct{}{} }
	session.startTimers()
	defer func() {
		session.Shutdown()
		session.stopTimers()
		_ = session.stopDataPlane()
	}()

	if plane.startCount() != 1 {
		t.Fatalf("monitor starts = %d, want 1", plane.startCount())
	}
	plane.emit(xfrmExpireEvent{spi: 0x9999})
	select {
	case <-rekeyed:
		t.Fatal("foreign SPI triggered rekey")
	case <-time.After(20 * time.Millisecond):
	}
	plane.emit(xfrmExpireEvent{spi: 0x1111})
	select {
	case <-rekeyed:
	case <-time.After(time.Second):
		t.Fatal("soft expire did not trigger CHILD_SA rekey")
	}
	plane.emit(xfrmExpireEvent{spi: 0x2222, hard: true})
	select {
	case <-down:
	case <-time.After(time.Second):
		t.Fatal("hard expire did not notify session down")
	}
}

func TestXFRMHardExpireWithoutCallbackIsTerminal(t *testing.T) {
	session := NewSession(&Config{})
	session.kernelDataPlane = &restoredXFRMPlane{outboundSPI: 7, inboundSPI: 8}
	session.handleXFRMExpire(xfrmExpireEvent{spi: 8, hard: true})
	if err := session.TerminalError(); err == nil || !strings.Contains(err.Error(), "hard expired") {
		t.Fatalf("terminal error = %v", err)
	}
	select {
	case <-session.ctx.Done():
	default:
		t.Fatal("hard expire did not cancel the session")
	}
}

type restoredTUN struct {
	mu     sync.Mutex
	name   string
	closes int
}

func (t *restoredTUN) Close() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.closes++
	return nil
}

func (t *restoredTUN) DeviceName() string               { return t.name }
func (t *restoredTUN) Read([]byte) (int, error)         { return 0, errors.New("test TUN is not running") }
func (t *restoredTUN) Write(packet []byte) (int, error) { return len(packet), nil }

func (t *restoredTUN) closeCount() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.closes
}

type restoredNetTools struct {
	mu         sync.Mutex
	ops        []string
	failPrefix string
	failErr    error
}

func (n *restoredNetTools) SetLinkUp(iface string) error   { return n.record("up:" + iface) }
func (n *restoredNetTools) SetLinkDown(iface string) error { return n.record("down:" + iface) }
func (n *restoredNetTools) SetMTU(iface string, mtu int) error {
	return n.record(fmt.Sprintf("mtu:%s:%d", iface, mtu))
}
func (n *restoredNetTools) AddAddress(iface, cidr string) error {
	return n.record("addr:" + iface + ":" + cidr)
}
func (n *restoredNetTools) DelAddress(iface, cidr string) error {
	return n.record("del-addr:" + iface + ":" + cidr)
}
func (n *restoredNetTools) AddAddress6(iface, cidr string) error {
	return n.record("addr6:" + iface + ":" + cidr)
}
func (n *restoredNetTools) DelAddress6(iface, cidr string) error {
	return n.record("del-addr6:" + iface + ":" + cidr)
}
func (n *restoredNetTools) AddRoute(cidr, gateway, iface string) error {
	return n.record("route:" + cidr + ":" + gateway + ":" + iface)
}
func (n *restoredNetTools) DelRoute(cidr, gateway, iface string) error {
	return n.record("del-route:" + cidr + ":" + gateway + ":" + iface)
}
func (n *restoredNetTools) AddRoute6(cidr, gateway, iface string) error {
	return n.record("route6:" + cidr + ":" + gateway + ":" + iface)
}
func (n *restoredNetTools) DelRoute6(cidr, gateway, iface string) error {
	return n.record("del-route6:" + cidr + ":" + gateway + ":" + iface)
}

func (n *restoredNetTools) record(operation string) error {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.ops = append(n.ops, operation)
	if n.failPrefix != "" && strings.HasPrefix(operation, n.failPrefix) {
		return n.failErr
	}
	return nil
}

func (n *restoredNetTools) operations() []string {
	n.mu.Lock()
	defer n.mu.Unlock()
	return append([]string(nil), n.ops...)
}

type restoredXFRMPlane struct {
	mu                      sync.Mutex
	handler                 func(xfrmExpireEvent)
	starts                  int
	outboundSPI, inboundSPI uint32
}

func (p *restoredXFRMPlane) Close() error                          { return nil }
func (p *restoredXFRMPlane) DeviceName() string                    { return "xfrm-test" }
func (p *restoredXFRMPlane) EnsureIPv6Enabled() error              { return nil }
func (p *restoredXFRMPlane) Rekey(*Session, *childSARuntime) error { return nil }
func (p *restoredXFRMPlane) CurrentSPIs() (uint32, uint32) {
	return p.outboundSPI, p.inboundSPI
}
func (p *restoredXFRMPlane) StartExpireMonitor(_ context.Context, handler func(xfrmExpireEvent)) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.starts++
	p.handler = handler
	return nil
}
func (p *restoredXFRMPlane) startCount() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.starts
}
func (p *restoredXFRMPlane) emit(event xfrmExpireEvent) {
	p.mu.Lock()
	handler := p.handler
	p.mu.Unlock()
	if handler != nil {
		handler(event)
	}
}

func containsInOrder(all, expected []string) bool {
	index := 0
	for _, value := range all {
		if index < len(expected) && value == expected[index] {
			index++
		}
	}
	return index == len(expected)
}
