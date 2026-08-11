package swu

import (
	"bytes"
	"context"
	"errors"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	enginecrypto "github.com/iniwex5/vowifi-go/engine/crypto"
	"github.com/iniwex5/vowifi-go/engine/ipsec"
)

var (
	_ InnerPacketEndpoint = (*userspaceInnerPacketEndpoint)(nil)
	_ InnerPacketIO       = (*userspaceInnerPacketEndpoint)(nil)
)

func TestUserspaceDataPlaneProductionPacketPath(t *testing.T) {
	session, transport := newUserspaceDataPlaneSession(t, newTestIKETransport())
	defer session.Shutdown()
	if err := session.startEstablishedDataPlane(); err != nil {
		t.Fatalf("startEstablishedDataPlane: %v", err)
	}
	endpoint := session.InnerPacketEndpoint()
	if endpoint == nil {
		t.Fatal("InnerPacketEndpoint returned nil after production start")
	}
	outbound := testIPv4Flow(session.innerIP, net.IPv4(8, 8, 8, 8))
	if err := endpoint.WritePacket(context.Background(), outbound); err != nil {
		t.Fatalf("WritePacket: %v", err)
	}
	assertOutboundESPPlaintext(t, transport, session, outbound)

	inbound := testIPv4Flow(net.IPv4(8, 8, 8, 8), session.innerIP)
	esp, err := ipsec.Encapsulate(inbound, session.espInboundSA)
	if err != nil {
		t.Fatalf("build inbound ESP: %v", err)
	}
	transport.esp <- esp
	assertInboundPlaintext(t, endpoint, inbound)
	assertUserspaceSnapshot(t, endpoint.Snapshot(), len(outbound), len(inbound))
}

func TestUserspaceEndpointCancellationAndSAErrorsAreExplicit(t *testing.T) {
	session := NewSession(&Config{})
	endpoint := newUserspaceInnerPacketEndpoint(session, newTestIKETransport(), 1)
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if err := endpoint.WritePacket(canceled, []byte{0x45}); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled WritePacket error = %v", err)
	}
	if err := endpoint.WritePacket(context.Background(), []byte{0x45}); !errors.Is(err, errInnerPacketSAMissing) {
		t.Fatalf("missing outbound SA error = %v", err)
	}
	unknownSPI := []byte{0x10, 0x20, 0x30, 0x40, 0, 0, 0, 1}
	endpoint.handleOuterESP(unknownSPI)
	snapshot := endpoint.Snapshot()
	if snapshot.SAMisses != 2 || snapshot.EncapsulationErrors != 0 || snapshot.DecapsulationErrors != 0 {
		t.Fatalf("classified snapshot = %+v", snapshot)
	}
}

func TestUserspaceEndpointReportsTransportSendFailure(t *testing.T) {
	sentinel := errors.New("send ESP failed")
	transport := &failingESPTransport{testIKETransport: newTestIKETransport(), err: sentinel}
	session, _ := newUserspaceDataPlaneSession(t, transport)
	endpoint := newUserspaceInnerPacketEndpoint(session, transport, 1)
	packet := testIPv4Flow(session.innerIP, net.IPv4(8, 8, 8, 8))
	if err := endpoint.WritePacket(context.Background(), packet); !errors.Is(err, sentinel) {
		t.Fatalf("WritePacket send error = %v", err)
	}
	snapshot := endpoint.Snapshot()
	if snapshot.SendErrors != 1 || snapshot.OutboundPackets != 0 {
		t.Fatalf("send failure snapshot = %+v", snapshot)
	}
}

func TestUserspaceEndpointCloseWaitsForInflightWrite(t *testing.T) {
	transport := newBlockingESPTransport()
	session, _ := newUserspaceDataPlaneSession(t, transport)
	endpoint := newUserspaceInnerPacketEndpoint(session, transport, 1)
	packet := testIPv4Flow(session.innerIP, net.IPv4(8, 8, 8, 8))
	writeDone := make(chan error, 1)
	go func() { writeDone <- endpoint.WritePacket(context.Background(), packet) }()
	<-transport.entered
	closeDone := make(chan error, 1)
	go func() { closeDone <- endpoint.Close() }()
	select {
	case err := <-closeDone:
		t.Fatalf("Close returned before SendESP completed: %v", err)
	case <-time.After(50 * time.Millisecond):
	}
	close(transport.release)
	if err := <-writeDone; err != nil {
		t.Fatalf("inflight WritePacket: %v", err)
	}
	if err := <-closeDone; err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := endpoint.WritePacket(context.Background(), packet); !errors.Is(err, errInnerPacketEndpointClosed) {
		t.Fatalf("post-close WritePacket error = %v", err)
	}
}

func TestUserspaceInboundQueueDropsWithoutBlocking(t *testing.T) {
	session, transport := newUserspaceDataPlaneSession(t, newTestIKETransport())
	endpoint := newUserspaceInnerPacketEndpoint(session, transport, 2)
	inner := testIPv4Flow(net.IPv4(8, 8, 8, 8), session.innerIP)
	for index := 0; index < 3; index++ {
		esp, err := ipsec.Encapsulate(inner, session.espInboundSA)
		if err != nil {
			t.Fatalf("build inbound ESP %d: %v", index, err)
		}
		endpoint.handleOuterESP(esp)
	}
	snapshot := endpoint.Snapshot()
	if snapshot.InboundPackets != 2 || snapshot.SendErrors == 0 {
		t.Fatalf("full queue snapshot = %+v", snapshot)
	}
}

func TestDecapsulateOuterESPReturnsSPIOnFailure(t *testing.T) {
	session, _ := newUserspaceDataPlaneSession(t, newTestIKETransport())
	packet := make([]byte, 8)
	packet[0], packet[1], packet[2], packet[3] = 0xde, 0xad, 0xbe, 0xef
	_, spi, err := session.decapsulateOuterESP(packet)
	if spi != 0xdeadbeef || err == nil {
		t.Fatalf("decapsulate failure spi=%08x err=%v", spi, err)
	}
	for _, expected := range []string{"spi=deadbeef", "current_local=10203040", "known_inbound=10203040"} {
		if !strings.Contains(err.Error(), expected) {
			t.Fatalf("decapsulate error %q omitted %q", err, expected)
		}
	}
}

func TestStartUserspaceDataPlaneRequiresRealTransport(t *testing.T) {
	session := NewSession(&Config{})
	if session.InnerPacketEndpoint() != nil || session.InnerPacketIO() != nil {
		t.Fatal("unstarted session exposed a typed-nil packet endpoint")
	}
	if err := session.startUserspaceDataPlane(); err == nil || err.Error() != "userspace data plane socket is nil" {
		t.Fatalf("startUserspaceDataPlane error = %v", err)
	}
}

func TestInnerPacketSnapshotMatchesOriginalCounterProjection(t *testing.T) {
	stats := dataPlaneRuntimeStats{}
	stats.espSend.Store(2)
	stats.tunWrite.Store(3)
	stats.espOutSAMiss.Store(5)
	stats.espInSAMiss.Store(7)
	stats.espEncapError.Store(11)
	stats.espDecapError.Store(13)
	stats.espSendError.Store(17)
	stats.tunWriteError.Store(19)
	stats.lastTunReadLen.Store(23)
	stats.lastPlainLen.Store(29)
	want := InnerPacketSnapshot{2, 3, 12, 11, 13, 17, 23, 29}
	if got := stats.innerPacketSnapshot(); got != want {
		t.Fatalf("inner packet snapshot = %+v, want %+v", got, want)
	}
}

func TestUserspaceEndpointClosesWithSessionContext(t *testing.T) {
	session, _ := newUserspaceDataPlaneSession(t, newTestIKETransport())
	defer session.Shutdown()
	if err := session.startUserspaceDataPlane(); err != nil {
		t.Fatalf("startUserspaceDataPlane: %v", err)
	}
	endpoint := session.InnerPacketEndpoint()
	session.cancel()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if _, err := endpoint.ReadPacket(ctx); !errors.Is(err, errInnerPacketEndpointClosed) {
		t.Fatalf("ReadPacket after session cancellation = %v", err)
	}
}

func TestUserspaceStartShutdownRaceLeavesNoWorkers(t *testing.T) {
	for iteration := 0; iteration < 25; iteration++ {
		session := NewSession(&Config{})
		session.setTransport(newTestIKETransport())
		startDone := make(chan error, 1)
		go func() { startDone <- session.startUserspaceDataPlane() }()
		session.Shutdown()
		<-startDone
		if session.currentInnerPacketEndpoint() != nil || session.transport() != nil {
			t.Fatalf("iteration %d retained endpoint or transport", iteration)
		}
		session.mu.RLock()
		started := session.dataPlaneStarted
		session.mu.RUnlock()
		if started {
			t.Fatalf("iteration %d retained started data plane", iteration)
		}
	}
}

func newUserspaceDataPlaneSession(t *testing.T, transport ipsec.Transport) (*Session, *testIKETransport) {
	t.Helper()
	session := NewSession(&Config{})
	session.setTransport(transport)
	session.ikeKeys = &IKEKeys{SK_d: bytes.Repeat([]byte{0x31}, enginecrypto.PRFOutputSize(session.prf))}
	session.innerIP, session.innerPrefix = net.IPv4(10, 0, 0, 2), 32
	session.espLocalSPI, session.espRemoteSPI = 0x10203040, 0x50607080
	session.childNi, session.childNr = bytes.Repeat([]byte{0x41}, 32), bytes.Repeat([]byte{0x42}, 32)
	session.childTSi, session.childTSr = buildTrafficSelectorsForIPStack(session.innerIP)
	if err := session.setupDataPlane(); err != nil {
		t.Fatalf("setupDataPlane: %v", err)
	}
	testTransport, _ := transport.(*testIKETransport)
	return session, testTransport
}

func assertOutboundESPPlaintext(t *testing.T, transport *testIKETransport, session *Session, want []byte) {
	t.Helper()
	select {
	case packet := <-transport.sentESP:
		plain, err := ipsec.Decapsulate(packet, session.espOutboundSA)
		if err != nil || !bytes.Equal(plain, want) {
			t.Fatalf("outbound ESP plaintext=%x err=%v", plain, err)
		}
	case <-time.After(time.Second):
		t.Fatal("production transport did not receive outbound ESP")
	}
}

func assertInboundPlaintext(t *testing.T, endpoint InnerPacketEndpoint, want []byte) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	packet, err := endpoint.ReadPacket(ctx)
	if err != nil || !bytes.Equal(packet, want) {
		t.Fatalf("inbound plaintext=%x err=%v", packet, err)
	}
	packet[0] ^= 0xff
	if bytes.Equal(packet, want) {
		t.Fatal("ReadPacket returned endpoint-owned storage")
	}
}

func assertUserspaceSnapshot(t *testing.T, snapshot InnerPacketSnapshot, outboundLen, inboundLen int) {
	t.Helper()
	if snapshot.OutboundPackets != 1 || snapshot.InboundPackets != 1 ||
		snapshot.LastOutboundLen != uint64(outboundLen) || snapshot.LastInboundLen != uint64(inboundLen) {
		t.Fatalf("userspace snapshot = %+v", snapshot)
	}
}

type blockingESPTransport struct {
	*testIKETransport
	entered chan struct{}
	release chan struct{}
	once    sync.Once
}

type failingESPTransport struct {
	*testIKETransport
	err error
}

func (t *failingESPTransport) SendESP([]byte) error { return t.err }

func newBlockingESPTransport() *blockingESPTransport {
	return &blockingESPTransport{
		testIKETransport: newTestIKETransport(),
		entered:          make(chan struct{}),
		release:          make(chan struct{}),
	}
}

func (t *blockingESPTransport) SendESP(packet []byte) error {
	t.once.Do(func() { close(t.entered) })
	<-t.release
	return t.testIKETransport.SendESP(packet)
}
