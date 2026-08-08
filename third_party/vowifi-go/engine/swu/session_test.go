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
	strict := buildAlgorithmPlan(&Config{AlgorithmPolicy: "strict", EnableLegacyCiphers: true})
	if strict.policyLabel() != AlgorithmPolicyStrict || strict.allowsEncryption(ikev2.ENCR_3DES) {
		t.Fatalf("strict plan = %+v", strict)
	}
	legacy := buildAlgorithmPlan(&Config{
		AlgorithmPolicy: AlgorithmPolicyLegacyPrefer, EnableLegacyCiphers: true,
		AllowedLegacyCiphers: []string{"3-des"},
	})
	if !legacy.allowsEncryption(ikev2.ENCR_3DES) || legacy.allowsEncryption(ikev2.ENCR_DES) {
		t.Fatalf("legacy plan = %+v", legacy)
	}

	cfg := &Config{IKEEncryption: 7}
	plan := buildExplicitAlgorithmPlan(cfg)
	if plan.IKEEncryption != 7 {
		t.Errorf("override: encr = %d, want 7", plan.IKEEncryption)
	}
}

func TestBuildESPProposals(t *testing.T) {
	proposals, err := buildESPProposals(&Config{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(proposals) != 4 {
		t.Fatalf("proposals = %d, want 4", len(proposals))
	}
	selection, err := firstESPAlgorithmSelection(proposals[0])
	if err != nil {
		t.Fatal(err)
	}
	encr, integ := selection.encryption, selection.integrity
	if encr != enginecrypto.EncrAESGCM16 || integ != 0 || selection.keyBits != 256 {
		t.Errorf("esp = encr %d/%d integ %d, want GCM256/NONE", encr, selection.keyBits, integ)
	}
}

func TestParseIKEProposal(t *testing.T) {
	proposals := explicitIKEProposals(&Config{IKEEncryption: 12, IKEPRF: 2, IKEIntegrity: 2, IKEDH: 14}, nil)
	selection, err := firstIKEAlgorithmSelection(proposals[0])
	if err != nil {
		t.Fatalf("firstIKEAlgorithmSelection: %v", err)
	}
	encr, prf, integ, dh := selection.encryption, selection.prf, selection.integrity, selection.dh
	if encr != 12 || prf != 2 || integ != 2 || dh != 14 {
		t.Errorf("ike = %d/%d/%d/%d, want 12/2/2/14", encr, prf, integ, dh)
	}
}

func TestPrioritizeDHGroup(t *testing.T) {
	proposal := ikev2.NewProposal(1, ikev2.ProtoIKE, nil)
	proposal.AddTransform(ikev2.TransformTypeDH, ikev2.MODP_1024_bit)
	proposal.AddTransform(ikev2.TransformTypeDH, ikev2.MODP_2048_bit)
	prioritizeDHGroup([]*ikev2.Proposal{proposal}, ikev2.MODP_2048_bit)
	if proposal.Transforms[0].ID != ikev2.MODP_2048_bit {
		t.Errorf("first = %d, want 14", proposal.Transforms[0].ID)
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
	got := buildNAI("310260123456789", &Config{MCC: "310", MNC: "26"})
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
	tsr := &ikev2.EncryptedPayloadTS{TrafficSelectors: []*ikev2.TrafficSelector{
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
	if len(tsi.TrafficSelectors) != 2 || len(tsr.TrafficSelectors) != 2 {
		t.Fatalf("unassigned selector counts = %d/%d, want IPv4 and IPv6", len(tsi.TrafficSelectors), len(tsr.TrafficSelectors))
	}
	if !bytes.Equal(tsi.TrafficSelectors[0].StartAddr, net.IPv4zero.To4()) ||
		!bytes.Equal(tsi.TrafficSelectors[0].EndAddr, net.IPv4bcast.To4()) {
		t.Fatalf("IPv4 any selector = %v..%v", tsi.TrafficSelectors[0].StartAddr, tsi.TrafficSelectors[0].EndAddr)
	}
	if bytes.Equal(tsi.TrafficSelectors[1].StartAddr, tsi.TrafficSelectors[1].EndAddr) ||
		!bytes.Equal(tsi.TrafficSelectors[1].EndAddr, bytes.Repeat([]byte{0xff}, net.IPv6len)) {
		t.Fatalf("IPv6 any selector = %x..%x", tsi.TrafficSelectors[1].StartAddr, tsi.TrafficSelectors[1].EndAddr)
	}

	innerIP := net.IPv4(10, 0, 0, 2)
	tsi, tsr = buildTrafficSelectorsForIPStack(innerIP)
	if !bytes.Equal(tsi.TrafficSelectors[0].StartAddr, innerIP.To4()) ||
		!bytes.Equal(tsi.TrafficSelectors[0].EndAddr, innerIP.To4()) {
		t.Fatalf("assigned TSi = %v..%v", tsi.TrafficSelectors[0].StartAddr, tsi.TrafficSelectors[0].EndAddr)
	}
	if !bytes.Equal(tsr.TrafficSelectors[0].EndAddr, net.IPv4bcast.To4()) {
		t.Fatalf("assigned TSr end = %v, want IPv4 broadcast", tsr.TrafficSelectors[0].EndAddr)
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
	if s.encrAlg != 12 || s.prfAlg != 5 || s.integAlg != 12 || s.dhGroup != 14 {
		t.Fatalf("IKE algorithms = %d/%d/%d/%d", s.encrAlg, s.prfAlg, s.integAlg, s.dhGroup)
	}
	if s.encKeyLen != 16 || s.integKeyLen != 32 {
		t.Fatalf("IKE key lengths = %d/%d", s.encKeyLen, s.integKeyLen)
	}
	if s.espCipher != 20 || s.espInteg != 0 || s.espEncKeyLen != 36 || s.espIntegKeyLen != 0 {
		t.Fatalf("ESP algorithms not initialized: %+v", s)
	}
}

func TestNewSessionStrictPolicyRejectsLegacyWithoutChangingProposalOrder(t *testing.T) {
	s := NewSession(&Config{AlgorithmPolicy: "strict"})
	if s.initErr != nil {
		t.Fatalf("initErr = %v", s.initErr)
	}
	if s.aead || !s.espAEAD || s.encKeyLen != 16 || s.espEncKeyLen != 36 {
		t.Fatalf("strict AEAD parameters = ike(%t,%d) esp(%t,%d)", s.aead, s.encKeyLen, s.espAEAD, s.espEncKeyLen)
	}
	if s.integKeyLen != 32 || s.espIntegKeyLen != 0 {
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

func TestNewSessionInitializesExplicitlyEnabledLegacyAlgorithms(t *testing.T) {
	s := NewSession(&Config{
		AlgorithmPolicy: AlgorithmPolicyLegacyPrefer, EnableLegacyCiphers: true,
		IKEProposals: []string{"3des-sha1-modp1024"},
		ESPProposals: []string{"3des-sha1"},
	})
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
