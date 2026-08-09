package netstack

import (
	"bytes"
	"context"
	"errors"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/iniwex5/vowifi-go/engine/swu"
	"github.com/iniwex5/vowifi-go/internal/vowifi/imscore"
	"github.com/iniwex5/vowifi-go/internal/vowifi/ipsec3gpp"
)

type endpointResult struct {
	packet []byte
	err    error
}

type testInnerEndpoint struct {
	reads     chan endpointResult
	writes    chan []byte
	done      chan struct{}
	closeOnce sync.Once
}

func newTestInnerEndpoint() *testInnerEndpoint {
	return &testInnerEndpoint{
		reads: make(chan endpointResult, 8), writes: make(chan []byte, 8), done: make(chan struct{}),
	}
}

func (e *testInnerEndpoint) ReadPacket(ctx context.Context) ([]byte, error) {
	select {
	case result := <-e.reads:
		return append([]byte(nil), result.packet...), result.err
	case <-e.done:
		return nil, errors.New("test endpoint closed")
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (e *testInnerEndpoint) WritePacket(ctx context.Context, packet []byte) error {
	select {
	case e.writes <- append([]byte(nil), packet...):
		return nil
	case <-e.done:
		return errors.New("test endpoint closed")
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (e *testInnerEndpoint) Snapshot() swu.InnerPacketSnapshot {
	return swu.InnerPacketSnapshot{}
}

func (e *testInnerEndpoint) Close() error {
	e.closeOnce.Do(func() { close(e.done) })
	return nil
}

type passthroughTransformer struct {
	mu               sync.Mutex
	inboundFailures  int
	outboundFailures int
	outboundCalls    int
}

func (t *passthroughTransformer) TransformInbound(packet []byte) ([]byte, bool, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.inboundFailures > 0 {
		t.inboundFailures--
		return nil, false, errors.New("inbound transform failed")
	}
	return append([]byte(nil), packet...), false, nil
}

func (t *passthroughTransformer) TransformOutbound(packet []byte) ([]byte, bool, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.outboundCalls++
	if t.outboundFailures > 0 {
		t.outboundFailures--
		return nil, false, errors.New("outbound transform failed")
	}
	return append([]byte(nil), packet...), false, nil
}

func TestOriginalNetworkConstructorConfiguresDualStack(t *testing.T) {
	endpoint := newTestInnerEndpoint()
	network, err := NewNetwork(
		context.Background(), net.IPv4(10, 0, 0, 2), net.ParseIP("2001:db8::2"),
		64, 0, endpoint, nil, []net.IP{net.IPv4(10, 0, 0, 53)},
	)
	if err != nil {
		t.Fatalf("NewNetwork: %v", err)
	}
	defer network.Close()

	if got := network.LocalIP(); !got.Equal(net.IPv4(10, 0, 0, 2)) {
		t.Fatalf("LocalIP = %v", got)
	}
	if !network.HasLocalIP(net.ParseIP("2001:db8::2")) {
		t.Fatal("HasLocalIP did not match configured IPv6 address")
	}
	if routes := network.stack.GetRouteTable(); len(routes) != 4 {
		t.Fatalf("route count = %d, want 4", len(routes))
	}
	servers := network.dnsServers()
	servers[0][0] ^= 0xff
	if network.dnsServers()[0].Equal(servers[0]) {
		t.Fatal("dnsServers returned mutable network storage")
	}
}

func TestOriginalNetworkConstructorRequiresLocalAddress(t *testing.T) {
	_, err := NewNetwork(context.Background(), nil, nil, 0, 0, nil, nil, nil)
	if err == nil || err.Error() != "netstack: at least one local IP is required" {
		t.Fatalf("NewNetwork error = %v", err)
	}
}

func TestPacketBridgeContinuesAfterInboundErrorAndIgnoresBoolean(t *testing.T) {
	endpoint := newTestInnerEndpoint()
	transformer := &passthroughTransformer{inboundFailures: 1, outboundFailures: 1}
	network := newOriginalTestNetwork(t, endpoint)
	defer network.Close()
	network.bridge.SetTransformer(transformer)

	endpoint.reads <- endpointResult{packet: []byte{0x45}}
	endpoint.reads <- endpointResult{packet: minimalIPv4Packet()}
	waitFor(t, func() bool {
		stats := network.Stats()
		return stats.Bridge.InboundErrors == 1 && stats.InboundPackets == 1
	})

	conn, err := network.DialContext(
		context.Background(), "udp", nil, "10.0.0.1:5060", imscore.DialOptions{},
	)
	if err != nil {
		t.Fatalf("DialContext: %v", err)
	}
	defer conn.Close()
	if _, err := conn.Write([]byte("DROP")); err != nil {
		t.Fatalf("first Write: %v", err)
	}
	if _, err := conn.Write([]byte("REGISTER")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	select {
	case packet := <-endpoint.writes:
		if !bytes.Contains(packet, []byte("REGISTER")) {
			t.Fatalf("unexpected outbound packet: %x", packet)
		}
	case <-time.After(time.Second):
		t.Fatal("outbound packet was dropped when transformer returned false")
	}
	waitFor(t, func() bool {
		stats := network.Stats()
		return stats.OutboundPackets == 1 && stats.Bridge.OutboundPackets == 1 &&
			stats.Bridge.OutboundErrors == 1
	})
}

func TestInstallIPSec3GPPCleanupRemovesTransformer(t *testing.T) {
	network := newOriginalTestNetwork(t, newTestInnerEndpoint())
	defer network.Close()
	cleanup, err := network.InstallIPSec3GPP(context.Background(), testIPSecPolicy())
	if err != nil {
		t.Fatalf("InstallIPSec3GPP: %v", err)
	}
	if !network.IPSec3GPPPolicyInstalled() || network.bridge.currentTransformer() == nil {
		t.Fatal("IPsec transformer was not installed")
	}
	if err := cleanup(); err != nil {
		t.Fatalf("cleanup: %v", err)
	}
	if network.IPSec3GPPPolicyInstalled() || network.bridge.currentTransformer() != nil {
		t.Fatal("IPsec transformer remained installed after cleanup")
	}
}

func TestInstallIPSec3GPPRequiresPacketBridge(t *testing.T) {
	network, err := NewNetwork(
		context.Background(), net.IPv4(10, 0, 0, 2), nil, 32, 0, nil, nil, nil,
	)
	if err != nil {
		t.Fatalf("NewNetwork: %v", err)
	}
	defer network.Close()
	_, err = network.InstallIPSec3GPP(context.Background(), testIPSecPolicy())
	if err == nil || err.Error() != "netstack: packet bridge is not available" {
		t.Fatalf("InstallIPSec3GPP error = %v", err)
	}
}

func newOriginalTestNetwork(t *testing.T, endpoint swu.InnerPacketEndpoint) *Network {
	t.Helper()
	network, err := NewNetwork(
		context.Background(), net.IPv4(10, 0, 0, 2), nil, 32, 0, endpoint, nil, nil,
	)
	if err != nil {
		t.Fatalf("NewNetwork: %v", err)
	}
	return network
}

func minimalIPv4Packet() []byte {
	packet := make([]byte, 20)
	packet[0] = 0x45
	packet[2], packet[3] = 0, byte(len(packet))
	packet[8] = 64
	packet[9] = 1
	copy(packet[12:16], net.IPv4(10, 0, 0, 1).To4())
	copy(packet[16:20], net.IPv4(10, 0, 0, 2).To4())
	return packet
}

func testIPSecPolicy() ipsec3gpp.Policy {
	return ipsec3gpp.Policy{
		LocalIP: net.IPv4(10, 0, 0, 2), RemoteIP: net.IPv4(10, 0, 0, 1),
		LocalClientPort: 41000, LocalServerPort: 41001,
		RemoteClientPort: 51000, RemoteServerPort: 51001,
		LocalClientSPI: 0x11111111, LocalServerSPI: 0x22222222,
		RemoteClientSPI: 0x33333333, RemoteServerSPI: 0x44444444,
		Authentication: ipsec3gpp.AuthHMACSHA196, Encryption: ipsec3gpp.EncryptionAES,
		Protocol: ipsec3gpp.ProtocolESP, Mode: ipsec3gpp.ModeTransport,
		CK: bytes.Repeat([]byte{0x11}, 16), IK: bytes.Repeat([]byte{0x22}, 16),
	}
}

func waitFor(t *testing.T, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("condition was not met before timeout")
}
