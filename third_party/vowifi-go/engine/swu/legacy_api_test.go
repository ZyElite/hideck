package swu

import (
	"bytes"
	"encoding/binary"
	"errors"
	"net"
	"testing"

	"github.com/iniwex5/vowifi-go/engine/ipsec"
	"go.uber.org/zap"
)

func TestLegacySessionPublicIKEStateTracksActiveState(t *testing.T) {
	injectedLogger := zap.NewNop()
	session := NewSession(&Config{}, injectedLogger)
	if session.Logger != injectedLogger {
		t.Fatal("NewSession did not retain the injected logger")
	}
	if session.EncAlg == nil || session.IntegAlg == nil || session.PRFAlg == nil || session.DH == nil {
		t.Fatal("NewSession did not initialize the public algorithm fields")
	}
	if session.SequenceNumber.Load() != 0 {
		t.Fatalf("initial public sequence = %d, want 0", session.SequenceNumber.Load())
	}

	session.spiI = ikeSPIBytes(0x0102030405060708)
	session.spiR = ikeSPIBytes(0x1112131415161718)
	session.nextOutboundID = 7
	session.ikeKeys = testIKEKeys()
	session.syncLegacyIKEStateLocked()
	if session.SPIi != binary.BigEndian.Uint64(session.spiI[:]) ||
		session.SPIr != binary.BigEndian.Uint64(session.spiR[:]) {
		t.Fatalf("public SPIs = %016x/%016x", session.SPIi, session.SPIr)
	}
	if session.SequenceNumber.Load() != 7 || session.Keys == nil ||
		!bytes.Equal(session.Keys.SK_ei, session.ikeKeys.SK_ei) {
		t.Fatal("public sequence or IKE keys did not track active state")
	}

	session.clearIKEKeyMaterial()
	if session.Keys != nil || session.DH != nil {
		t.Fatal("public IKE key material remained after cleanup")
	}
}

func TestLegacySnapshotIsStructuredAndDetached(t *testing.T) {
	session := NewSession(&Config{TUNMTU: 1420})
	session.mu.Lock()
	session.state = stateEstablished
	session.innerIP = net.IPv4(10, 0, 0, 8)
	session.innerIPv6 = net.ParseIP("2001:db8::8")
	session.innerIPv6Prefix = 56
	session.dnsServers = []net.IP{net.IPv4(1, 1, 1, 1), net.ParseIP("2001:4860:4860::8888")}
	session.pcscfServers = []net.IP{net.IPv4(192, 0, 2, 8), net.ParseIP("2001:db8::20")}
	session.offeredIKEProfiles = []string{"sha2_modern", "sha1_legacy"}
	session.effectiveCipherPolicy = AlgorithmPolicyBalanced
	session.negotiationFallbackCount = 1
	session.terminalErr = errors.New("retained diagnostic")
	session.mu.Unlock()
	session.childSAMu.Lock()
	session.espInboundSA = &ipsec.SecurityAssociation{SPI: 1}
	session.espOutboundSA = &ipsec.SecurityAssociation{SPI: 2}
	session.espInboundSAs[1] = session.espInboundSA
	session.syncLegacyChildStateLocked()
	session.childSAMu.Unlock()

	snapshot := session.Snapshot()
	if !snapshot.Established || snapshot.MTU != 1420 || snapshot.IPv6Prefix != 56 {
		t.Fatalf("snapshot lifecycle/network = %+v", snapshot)
	}
	if snapshot.LastError != "retained diagnostic" || snapshot.IKEProfile == "" ||
		len(snapshot.DNSv4) != 1 || len(snapshot.DNSv6) != 1 ||
		len(snapshot.PCSCFv4) != 1 || len(snapshot.PCSCFv6) != 1 {
		t.Fatalf("snapshot diagnostics/address families = %+v", snapshot)
	}
	snapshot.IPv4[0] ^= 0xff
	snapshot.DNSv4[0][0] ^= 0xff
	snapshot.OfferedIKEProfiles[0] = "mutated"
	if session.innerIP.Equal(snapshot.IPv4) || session.dnsServers[0].Equal(snapshot.DNSv4[0]) ||
		session.offeredIKEProfiles[0] == snapshot.OfferedIKEProfiles[0] {
		t.Fatal("Snapshot returned aliases to session-owned state")
	}
	if session.SnapshotMap()["state"] != stateEstablished {
		t.Fatal("additive SnapshotMap did not retain the current status API")
	}
	session.Shutdown()
}

func TestSessionSnapshotConcurrentWithDataPlaneStop(t *testing.T) {
	session := NewSession(&Config{})
	session.swapTUN(&restoredTUN{name: "swu-snapshot"})
	session.mu.Lock()
	session.state = stateEstablished
	session.mu.Unlock()

	started := make(chan struct{})
	done := make(chan struct{})
	go func() {
		close(started)
		for range 1000 {
			_ = session.Snapshot()
		}
		close(done)
	}()
	<-started
	if err := session.stopDataPlane(); err != nil {
		t.Fatalf("stopDataPlane: %v", err)
	}
	<-done
	if got := session.Snapshot().TUNName; got != "" {
		t.Fatalf("TUNName after stop = %q, want empty", got)
	}
}

func TestLegacySnapshotUsesCommittedChildSAState(t *testing.T) {
	session := NewSession(&Config{})
	session.childSAMu.Lock()
	session.espInboundSA = &ipsec.SecurityAssociation{SPI: 1}
	session.espOutboundSA = &ipsec.SecurityAssociation{SPI: 2}
	session.childSAMu.Unlock()
	if session.State() != stateIdle {
		t.Fatalf("precondition state = %q", session.State())
	}
	if !session.Snapshot().Established {
		t.Fatal("committed CHILD SAs were hidden until the state callback")
	}
}
