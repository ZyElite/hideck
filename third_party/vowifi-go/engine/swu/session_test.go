package swu

import (
	"bytes"
	"context"
	"errors"
	"net"
	"strings"
	"testing"
	"time"

	enginecrypto "github.com/iniwex5/vowifi-go/engine/crypto"
	"github.com/iniwex5/vowifi-go/engine/ikev2"
)

func TestBuildAlgorithmPlan(t *testing.T) {
	cases := map[string]struct {
		policy string
		encr   uint16
		prf    uint16
	}{
		"strict":        {policy: "strict", encr: enginecrypto.EncrAESGCM16, prf: 5},
		"legacy_prefer": {policy: "legacy_prefer", encr: 3, prf: 2},
		"prefer":        {policy: "prefer", encr: 12, prf: 2},
		"":              {policy: "", encr: 12, prf: 2},
	}
	for name, tc := range cases {
		plan := buildAlgorithmPlan(tc.policy, nil)
		if plan.IKEEncryption != tc.encr || plan.IKEPRF != tc.prf {
			t.Errorf("%s: plan = encr %d prf %d, want encr %d prf %d",
				name, plan.IKEEncryption, plan.IKEPRF, tc.encr, tc.prf)
		}
	}
	// Explicit config overrides the policy.
	cfg := &Config{IKEEncryption: 7}
	plan := buildAlgorithmPlan("strict", cfg)
	if plan.IKEEncryption != 7 {
		t.Errorf("override: encr = %d, want 7", plan.IKEEncryption)
	}
}

func TestBuildESPProposals(t *testing.T) {
	proposals := buildESPProposals(0, 0)
	if len(proposals) != 1 {
		t.Fatalf("proposals = %d, want 1", len(proposals))
	}
	encr, integ, err := parseESPProposal(proposals[0])
	if err != nil {
		t.Fatalf("parseESPProposal: %v", err)
	}
	if encr != 12 || integ != 2 {
		t.Errorf("esp = encr %d integ %d, want 12/2", encr, integ)
	}
}

func TestParseIKEProposal(t *testing.T) {
	proposals := buildIKEProposals(12, 2, 2, 14)
	encr, prf, integ, dh, err := parseIKEProposal(proposals[0])
	if err != nil {
		t.Fatalf("parseIKEProposal: %v", err)
	}
	if encr != 12 || prf != 2 || integ != 2 || dh != 14 {
		t.Errorf("ike = %d/%d/%d/%d, want 12/2/2/14", encr, prf, integ, dh)
	}
}

func TestPrioritizeDHGroup(t *testing.T) {
	got := prioritizeDHGroup([]uint16{2, 14, 19}, 14)
	if got[0] != 14 {
		t.Errorf("first = %d, want 14", got[0])
	}
	if len(got) != 3 {
		t.Errorf("len = %d, want 3", len(got))
	}
}

func TestSpoofAppleIMEI(t *testing.T) {
	// 14-digit prefix; the check digit must make the IMEI Luhn-valid.
	imei := spoofAppleIMEI("35693803564380")
	if len(imei) != 15 {
		t.Fatalf("imei len = %d, want 15", len(imei))
	}
	sum := 0
	for i := 0; i < 15; i++ {
		d := int(imei[i] - '0')
		if i%2 == 1 {
			d *= 2
			if d > 9 {
				d -= 9
			}
		}
		sum += d
	}
	if sum%10 != 0 {
		t.Errorf("imei %s fails Luhn check", imei)
	}
}

func TestBuildNAIWithOverride(t *testing.T) {
	got := buildNAI("310260123456789", "310", "26")
	if got != "0310260123456789@nai.epc.mnc026.mcc310.3gppnetwork.org" {
		t.Errorf("NAI = %q", got)
	}
}

func TestExtractDstTuple(t *testing.T) {
	// IPv4 packet: version 4, dst at bytes 16-20.
	pkt := make([]byte, 20)
	pkt[0] = 0x45
	copy(pkt[16:20], net.IPv4(10, 0, 0, 1).To4())
	dst, _, err := extractDstTuple(pkt)
	if err != nil {
		t.Fatalf("extractDstTuple: %v", err)
	}
	if !dst.Equal(net.IPv4(10, 0, 0, 1)) {
		t.Errorf("dst = %v", dst)
	}
}

func TestInnerEndpointKeepsNetworkAndHostDirectionsSeparate(t *testing.T) {
	endpoint := newUserspaceInnerPacketEndpoint(1, 1)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	networkPacket := []byte("from-network")
	if err := endpoint.deliverNetworkPacket(ctx, networkPacket); err != nil {
		t.Fatalf("deliverNetworkPacket: %v", err)
	}
	got, err := endpoint.ReadPacketContext(ctx)
	if err != nil || !bytes.Equal(got, networkPacket) {
		t.Fatalf("network packet = %q, %v", got, err)
	}

	hostPacket := []byte("from-host")
	if err := endpoint.WritePacketContext(ctx, hostPacket); err != nil {
		t.Fatalf("WritePacketContext: %v", err)
	}
	select {
	case got = <-endpoint.hostPackets:
		if !bytes.Equal(got, hostPacket) {
			t.Fatalf("host packet = %q", got)
		}
	case <-ctx.Done():
		t.Fatal("host packet did not reach outbound queue")
	}
}

func TestMatchSelectors(t *testing.T) {
	tsr := &ikev2.EncryptedPayloadTS{Selectors: []*ikev2.TrafficSelector{
		ikev2.NewTrafficSelectorIPV4(net.IPv4zero, 0, 0, 0xffff),
	}}
	pkt := make([]byte, 20)
	pkt[0] = 0x45
	if !matchSelectors(pkt, nil, tsr) {
		t.Error("IPv4 packet should match IPv4 selector")
	}
}

func TestTrafficSelectorsUseFullAnyRanges(t *testing.T) {
	tsi, tsr := buildTrafficSelectorsForIPStack(nil)
	if len(tsi.Selectors) != 2 || len(tsr.Selectors) != 2 {
		t.Fatalf("unassigned selector counts = %d/%d, want IPv4 and IPv6", len(tsi.Selectors), len(tsr.Selectors))
	}
	if !bytes.Equal(tsi.Selectors[0].StartAddr, net.IPv4zero.To4()) ||
		!bytes.Equal(tsi.Selectors[0].EndAddr, net.IPv4bcast.To4()) {
		t.Fatalf("IPv4 any selector = %v..%v", tsi.Selectors[0].StartAddr, tsi.Selectors[0].EndAddr)
	}
	if bytes.Equal(tsi.Selectors[1].StartAddr, tsi.Selectors[1].EndAddr) ||
		!bytes.Equal(tsi.Selectors[1].EndAddr, bytes.Repeat([]byte{0xff}, net.IPv6len)) {
		t.Fatalf("IPv6 any selector = %x..%x", tsi.Selectors[1].StartAddr, tsi.Selectors[1].EndAddr)
	}

	innerIP := net.IPv4(10, 0, 0, 2)
	tsi, tsr = buildTrafficSelectorsForIPStack(innerIP)
	if !bytes.Equal(tsi.Selectors[0].StartAddr, innerIP.To4()) ||
		!bytes.Equal(tsi.Selectors[0].EndAddr, innerIP.To4()) {
		t.Fatalf("assigned TSi = %v..%v", tsi.Selectors[0].StartAddr, tsi.Selectors[0].EndAddr)
	}
	if !bytes.Equal(tsr.Selectors[0].EndAddr, net.IPv4bcast.To4()) {
		t.Fatalf("assigned TSr end = %v, want IPv4 broadcast", tsr.Selectors[0].EndAddr)
	}
}

func TestSessionStateTransitions(t *testing.T) {
	s := NewSession(&Config{IMSI: "310260123456789"})
	if s.State() != stateIdle {
		t.Errorf("initial state = %q", s.State())
	}
	s.setState(stateConnecting)
	if s.State() != stateConnecting {
		t.Errorf("state = %q", s.State())
	}
	s.setTerminalError(errors.New("boom"))
	if s.State() != stateError {
		t.Errorf("state = %q", s.State())
	}
	if s.TerminalError() == nil {
		t.Error("terminal error not recorded")
	}
	s.Shutdown()
	select {
	case <-s.done:
	default:
		t.Error("done channel not closed after Shutdown")
	}
}

func TestIKEReauthTimerSignalsFreshRuntimeRequirement(t *testing.T) {
	s := NewSession(&Config{ReauthSeconds: time.Millisecond})
	s.setState(stateEstablished)
	s.startIKEReauthTimer()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := s.WaitDoneContext(ctx); err != nil {
		t.Fatalf("WaitDoneContext: %v", err)
	}
	if s.State() != stateError {
		t.Fatalf("state = %q, want error", s.State())
	}
	if err := s.TerminalError(); !errors.Is(err, ErrFreshRuntimeRequired) {
		t.Fatalf("terminal error = %v, want fresh runtime requirement", err)
	}
	s.Shutdown()
}

func TestNewSessionInitializesDefaultAlgorithms(t *testing.T) {
	s := NewSession(&Config{})
	if s.initErr != nil {
		t.Fatalf("initErr = %v", s.initErr)
	}
	if s.dh == nil || s.prf == nil {
		t.Fatal("NewSession did not initialize DH and PRF")
	}
	if s.encrAlg != 12 || s.prfAlg != 2 || s.integAlg != 2 || s.dhGroup != 14 {
		t.Fatalf("IKE algorithms = %d/%d/%d/%d", s.encrAlg, s.prfAlg, s.integAlg, s.dhGroup)
	}
	if s.encKeyLen != 16 || s.integKeyLen != 20 {
		t.Fatalf("IKE key lengths = %d/%d", s.encKeyLen, s.integKeyLen)
	}
	if s.espCipher != 12 || s.espInteg != 2 || s.espEncKeyLen != 16 || s.espIntegKeyLen != 20 {
		t.Fatalf("ESP algorithms not initialized: %+v", s)
	}
}

func TestNewSessionInitializesStrictAEADAlgorithms(t *testing.T) {
	s := NewSession(&Config{AlgorithmPolicy: "strict"})
	if s.initErr != nil {
		t.Fatalf("initErr = %v", s.initErr)
	}
	if !s.aead || !s.espAEAD || s.encKeyLen != 20 || s.espEncKeyLen != 20 {
		t.Fatalf("strict AEAD parameters = ike(%t,%d) esp(%t,%d)", s.aead, s.encKeyLen, s.espAEAD, s.espEncKeyLen)
	}
	if s.integKeyLen != 0 || s.espIntegKeyLen != 0 {
		t.Fatalf("strict integrity key lengths = %d/%d", s.integKeyLen, s.espIntegKeyLen)
	}
}

func TestNewSessionInitializesAES256SHA512Algorithms(t *testing.T) {
	s := NewSession(&Config{
		IKEEncryption: 12, IKEEncryptionKeyBits: 256, IKEPRF: 7, IKEIntegrity: 14, IKEDH: 14,
		ESPEncryption: 12, ESPEncryptionKeyBits: 256, ESPIntegrity: 14,
	})
	if s.initErr != nil {
		t.Fatalf("initErr = %v", s.initErr)
	}
	if s.encKeyLen != 32 || s.integKeyLen != 64 || s.espEncKeyLen != 32 || s.espIntegKeyLen != 64 {
		t.Fatalf("AES256/SHA512 key lengths = ike(%d,%d) esp(%d,%d)",
			s.encKeyLen, s.integKeyLen, s.espEncKeyLen, s.espIntegKeyLen)
	}
}

func TestNewSessionRejectsUnsupportedGCMTagLengths(t *testing.T) {
	for _, transform := range []uint16{18, 19} {
		s := NewSession(&Config{IKEEncryption: transform, IKEIntegrity: 0})
		if s.initErr == nil || !strings.Contains(s.initErr.Error(), "non-16-byte GCM tag") {
			t.Fatalf("transform %d initErr = %v, want explicit tag-length error", transform, s.initErr)
		}
	}
}

func TestNewSessionInitializesLegacyAlgorithms(t *testing.T) {
	s := NewSession(&Config{AlgorithmPolicy: "legacy_prefer"})
	if s.initErr != nil {
		t.Fatalf("initErr = %v", s.initErr)
	}
	if s.encrAlg != 3 || s.encKeyLen != 24 || s.dhGroup != 2 {
		t.Fatalf("legacy parameters = encr=%d key=%d dh=%d", s.encrAlg, s.encKeyLen, s.dhGroup)
	}
}

func TestConnectReportsAlgorithmInitializationFailure(t *testing.T) {
	s := NewSession(&Config{EPDGAddr: "127.0.0.1", IKEDH: 999})
	err := s.Connect(context.Background())
	if err == nil || !strings.Contains(err.Error(), "不支持的 DH 组: 999") {
		t.Fatalf("Connect() error = %v", err)
	}
	if s.socket != nil || s.State() != stateError {
		t.Fatalf("failed initialization opened transport or missed error state")
	}
}

func TestStartEstablishedDataPlaneMarksRunning(t *testing.T) {
	s := NewSession(&Config{})
	s.socket = newTestIKETransport()
	s.innerEndpoint = newUserspaceInnerPacketEndpoint(1, 1)
	if err := s.startEstablishedDataPlane(); err != nil {
		t.Fatalf("startEstablishedDataPlane() error = %v", err)
	}
	s.mu.RLock()
	started := s.dataPlaneStarted
	s.mu.RUnlock()
	if !started {
		t.Fatal("data plane not marked started")
	}
	s.Shutdown()
}

func TestShutdownWaitsForDataPlaneBeforeClearingTransport(t *testing.T) {
	s := NewSession(&Config{})
	s.socket = newTestIKETransport()
	s.innerEndpoint = newUserspaceInnerPacketEndpoint(1, 1)
	if err := s.startEstablishedDataPlane(); err != nil {
		t.Fatalf("startEstablishedDataPlane() error = %v", err)
	}
	s.Shutdown()
	if s.socket != nil {
		t.Fatal("Shutdown did not clear transport")
	}
	s.mu.RLock()
	started := s.dataPlaneStarted
	s.mu.RUnlock()
	if started {
		t.Fatal("Shutdown left data plane marked started")
	}
}

func TestSessionManager(t *testing.T) {
	m := NewSessionManager()
	s := NewSession(&Config{IMSI: "310260123456789"})
	m.Start("dev-1", s)
	if m.Get("dev-1") != s {
		t.Error("Get did not return the session")
	}
	m.Stop("dev-1")
	if m.Get("dev-1") != nil {
		t.Error("Get after Stop should be nil")
	}
}

func TestFragmentMessage(t *testing.T) {
	s := NewSession(&Config{})
	raw := bytes.Repeat([]byte{0xaa}, 3000)
	if !s.shouldFragment(raw) {
		t.Error("3000-byte message should fragment")
	}
	parts, err := s.fragmentMessage(raw)
	if err != nil {
		t.Fatalf("fragmentMessage: %v", err)
	}
	var joined []byte
	for _, p := range parts {
		joined = append(joined, p...)
	}
	if !bytes.Equal(joined, raw) {
		t.Error("fragments do not reassemble")
	}
}
