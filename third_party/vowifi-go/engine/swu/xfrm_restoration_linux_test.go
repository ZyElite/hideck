//go:build linux

package swu

import (
	"context"
	"errors"
	"net"
	"reflect"
	"testing"
	"time"

	"github.com/iniwex5/netlink"
	"github.com/iniwex5/vowifi-go/engine/driver"
)

var _ xfrmManager = (*recordedXFRMManager)(nil)

func TestXFRMPoliciesRestoreOriginalFivePolicySet(t *testing.T) {
	outbound := driver.XFRMSAConfig{
		Src: net.ParseIP("192.0.2.1"), Dst: net.ParseIP("192.0.2.2"), SPI: 0x1111,
	}
	inbound := driver.XFRMSAConfig{
		Src: net.ParseIP("192.0.2.2"), Dst: net.ParseIP("192.0.2.1"), SPI: 0x2222,
	}
	policies := xfrmPolicies(outbound, inbound, 42)
	if len(policies) != 5 {
		t.Fatalf("policy count = %d, want 5", len(policies))
	}
	wantDirections := []netlink.Dir{
		netlink.XFRM_DIR_OUT, netlink.XFRM_DIR_IN, netlink.XFRM_DIR_FWD,
		netlink.XFRM_DIR_OUT, netlink.XFRM_DIR_IN,
	}
	for index, policy := range policies {
		if policy.Dir != wantDirections[index] || policy.Ifid != 42 {
			t.Fatalf("policy %d = dir %v ifid %d", index, policy.Dir, policy.Ifid)
		}
	}
	wantSPIs := []int{int(outbound.SPI), int(inbound.SPI), 0, int(outbound.SPI), int(inbound.SPI)}
	for index, policy := range policies {
		if policy.TmplSPI != wantSPIs[index] {
			t.Fatalf("policy %d SPI = %x, want %x", index, policy.TmplSPI, wantSPIs[index])
		}
	}
	if policies[2].Src.String() != "0.0.0.0/0" {
		t.Fatalf("forward policy network = %s, want IPv4", policies[2].Src)
	}
	for _, policy := range policies {
		if policy.Dir == netlink.XFRM_DIR_FWD && policy.Src.String() == "::/0" {
			t.Fatal("unexpected IPv6 forward policy")
		}
	}
}

func TestInstallXFRMPoliciesDistinguishesRequiredAndOptional(t *testing.T) {
	set := testXFRMPolicySet()
	requiredErr := errors.New("required policy rejected")
	required := &recordedXFRMManager{addSPFailures: map[int]error{1: requiredErr}}
	if err := installXFRMPolicies(required, set); !errors.Is(err, requiredErr) {
		t.Fatalf("required policy error = %v", err)
	}
	if len(required.addSPCalls) != 2 {
		t.Fatalf("required failure attempts = %d, want 2", len(required.addSPCalls))
	}

	optional := &recordedXFRMManager{addSPFailures: map[int]error{
		2: errors.New("forward unsupported"), 3: errors.New("IPv6 unsupported"),
	}}
	if err := installXFRMPolicies(optional, set); err != nil {
		t.Fatalf("optional policy error escaped: %v", err)
	}
	if len(optional.addSPCalls) != 5 {
		t.Fatalf("optional failure attempts = %d, want 5", len(optional.addSPCalls))
	}
}

func TestXFRMRekeyFailureRestoresPoliciesAndDeletesNewStates(t *testing.T) {
	sentinel := errors.New("inbound policy update rejected")
	manager := &recordedXFRMManager{updateSPFailures: map[int]error{1: sentinel}}
	plane := testXFRMPlane(manager)
	session := NewSession(&Config{DataplaneMode: DataplaneModeXFRMI})
	runtime := &childSARuntime{
		localSPI: 0x3030, remoteSPI: 0x4040,
		outboundKeys: childDirectionKeys{enc: make([]byte, session.espEncKeyLen), integ: make([]byte, session.espIntegKeyLen)},
		inboundKeys:  childDirectionKeys{enc: make([]byte, session.espEncKeyLen), integ: make([]byte, session.espIntegKeyLen)},
	}
	err := plane.Rekey(session, runtime)
	if !errors.Is(err, sentinel) {
		t.Fatalf("Rekey error = %v, want %v", err, sentinel)
	}
	if outbound, inbound := plane.CurrentSPIs(); outbound != 0x1010 || inbound != 0x2020 {
		t.Fatalf("active SPIs changed to %x/%x", outbound, inbound)
	}
	if got, want := manager.deletedSPIs, []uint32{0x4040, 0x3030}; !reflect.DeepEqual(got, want) {
		t.Fatalf("deleted SPIs = %x, want %x", got, want)
	}
	if len(manager.updateSPCalls) != 7 {
		t.Fatalf("policy update calls = %d, want failed pair plus five-policy rollback", len(manager.updateSPCalls))
	}
}

func TestXFRMMOBIKEStateInstallFailureRollsBackAddedStates(t *testing.T) {
	sentinel := errors.New("second state rejected")
	manager := &recordedXFRMManager{addSAFailures: map[int]error{1: sentinel}}
	plane := &xfrmDataPlane{manager: manager}
	states := []driver.XFRMSAConfig{{SPI: 0x11}, {SPI: 0x22}, {SPI: 0x33}}
	if err := plane.addMOBIKEStates(states); !errors.Is(err, sentinel) {
		t.Fatalf("addMOBIKEStates error = %v", err)
	}
	if got, want := manager.deletedSPIs, []uint32{0x11}; !reflect.DeepEqual(got, want) {
		t.Fatalf("MOBIKE rollback SPIs = %x, want %x", got, want)
	}
}

func TestXFRMExpireMonitorConvertsEventsAndStops(t *testing.T) {
	events := make(chan netlink.XfrmMsg, 1)
	monitorErrors := make(chan error, 1)
	doneObserved := make(chan struct{})
	plane := &xfrmDataPlane{monitorOpen: func(done <-chan struct{}) (<-chan netlink.XfrmMsg, <-chan error, error) {
		go func() {
			<-done
			close(doneObserved)
		}()
		return events, monitorErrors, nil
	}}
	received := make(chan xfrmExpireEvent, 1)
	if err := plane.StartExpireMonitor(context.Background(), func(event xfrmExpireEvent) {
		received <- event
	}); err != nil {
		t.Fatalf("StartExpireMonitor: %v", err)
	}
	events <- &netlink.XfrmMsgExpire{XfrmState: &netlink.XfrmState{Spi: 0x7788}, Hard: true}
	select {
	case event := <-received:
		if event.spi != 0x7788 || !event.hard {
			t.Fatalf("expire event = %+v", event)
		}
	case <-time.After(time.Second):
		t.Fatal("monitor did not deliver expire event")
	}
	plane.stopExpireMonitor()
	select {
	case <-doneObserved:
	case <-time.After(time.Second):
		t.Fatal("monitor stop did not close its done signal")
	}
}

func TestXFRMDataPlaneCloseIsIdempotentAndFinal(t *testing.T) {
	manager := &recordedXFRMManager{}
	networkRollbacks := 0
	socketCleanups := 0
	plane := testXFRMPlane(manager)
	plane.name = "xfrm-test"
	plane.rollbackNetwork = func() error {
		networkRollbacks++
		return nil
	}
	plane.disableUDPEncap = func() error {
		socketCleanups++
		return nil
	}
	if err := plane.Close(); err != nil {
		t.Fatalf("first Close: %v", err)
	}
	if err := plane.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
	if networkRollbacks != 1 || manager.cleanupCalls != 1 || socketCleanups != 1 {
		t.Fatalf("cleanup counts = network %d manager %d socket %d",
			networkRollbacks, manager.cleanupCalls, socketCleanups)
	}
	if outbound, inbound := plane.CurrentSPIs(); outbound != 0 || inbound != 0 {
		t.Fatalf("closed SPIs = %x/%x", outbound, inbound)
	}
	if err := plane.StartExpireMonitor(context.Background(), func(xfrmExpireEvent) {}); err == nil {
		t.Fatal("closed data plane restarted its expire monitor")
	}
}

func TestXFRMSAUsesNegotiatedESNAndRouteValidation(t *testing.T) {
	session := NewSession(&Config{DataplaneMode: DataplaneModeXFRMI, EnableESN: true})
	keys := &childSAKeys{
		initiator: childDirectionKeys{enc: make([]byte, session.espEncKeyLen), integ: make([]byte, session.espIntegKeyLen)},
		responder: childDirectionKeys{enc: make([]byte, session.espEncKeyLen), integ: make([]byte, session.espIntegKeyLen)},
	}
	outbound, inbound, err := session.xfrmSAConfigsFor(xfrmSAConfigSpec{
		keys: keys, localIP: net.ParseIP("192.0.2.1"), remoteIP: net.ParseIP("192.0.2.2"),
		localPort: 4500, remotePort: 4500, ifID: 9, localSPI: 1, remoteSPI: 2,
	})
	if err != nil {
		t.Fatalf("xfrmSAConfigsFor: %v", err)
	}
	if !outbound.ESN || !inbound.ESN {
		t.Fatal("negotiated ESN was not installed in both XFRM states")
	}
	if _, _, _, err := detectOutboundRoute(nil); err == nil {
		t.Fatal("nil XFRM remote route was accepted")
	}
}

func testXFRMPolicySet() xfrmPolicySet {
	return xfrmPolicySet{
		outbound: driver.XFRMSAConfig{Src: net.ParseIP("192.0.2.1"), Dst: net.ParseIP("192.0.2.2"), SPI: 0x1010},
		inbound:  driver.XFRMSAConfig{Src: net.ParseIP("192.0.2.2"), Dst: net.ParseIP("192.0.2.1"), SPI: 0x2020},
		ifID:     9,
	}
}

func testXFRMPlane(manager xfrmManager) *xfrmDataPlane {
	set := testXFRMPolicySet()
	return &xfrmDataPlane{
		manager: manager, localIP: cloneIP(set.outbound.Src), remoteIP: cloneIP(set.outbound.Dst),
		localPort: 4500, remotePort: 4500, ifID: set.ifID,
		outbound: set.outbound, inbound: set.inbound,
		retiredInbound: make(map[uint32]driver.XFRMSAConfig),
	}
}

type recordedXFRMManager struct {
	addSPCalls, updateSPCalls []driver.XFRMSPConfig
	addSACalls                []driver.XFRMSAConfig
	deletedSPIs               []uint32
	addSPFailures             map[int]error
	updateSPFailures          map[int]error
	addSAFailures             map[int]error
	cleanupCalls              int
}

func (*recordedXFRMManager) AddXFRMInterface(string, uint32, ...int) error { return nil }
func (*recordedXFRMManager) DelXFRMInterface(string) error                 { return nil }
func (m *recordedXFRMManager) AddSA(value any) error {
	config := value.(driver.XFRMSAConfig)
	index := len(m.addSACalls)
	m.addSACalls = append(m.addSACalls, config)
	return m.addSAFailures[index]
}
func (*recordedXFRMManager) UpdateSA(any) error { return nil }
func (m *recordedXFRMManager) DelSA(arguments ...any) error {
	m.deletedSPIs = append(m.deletedSPIs, arguments[0].(uint32))
	return nil
}
func (m *recordedXFRMManager) AddSP(value any) error {
	index := len(m.addSPCalls)
	m.addSPCalls = append(m.addSPCalls, value.(driver.XFRMSPConfig))
	return m.addSPFailures[index]
}
func (m *recordedXFRMManager) UpdateSP(value any) error {
	index := len(m.updateSPCalls)
	m.updateSPCalls = append(m.updateSPCalls, value.(driver.XFRMSPConfig))
	return m.updateSPFailures[index]
}
func (*recordedXFRMManager) DelSP(any) error { return nil }
func (m *recordedXFRMManager) CleanupChecked() error {
	m.cleanupCalls++
	return nil
}
