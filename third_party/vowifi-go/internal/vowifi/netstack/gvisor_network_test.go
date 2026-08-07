package netstack

import (
	"bytes"
	"context"
	"net"
	"testing"
	"time"
)

type channelPacketIO struct {
	inbound  chan []byte
	outbound chan []byte
}

func newChannelPacketIO() *channelPacketIO {
	return &channelPacketIO{inbound: make(chan []byte, 1), outbound: make(chan []byte, 1)}
}

func (p *channelPacketIO) ReadPacketContext(ctx context.Context) ([]byte, error) {
	select {
	case packet := <-p.inbound:
		return packet, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (p *channelPacketIO) WritePacketContext(ctx context.Context, packet []byte) error {
	select {
	case p.outbound <- append([]byte(nil), packet...):
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func TestTunnelNetworkSendsUDPThroughPacketIO(t *testing.T) {
	packetIO := newChannelPacketIO()
	network, err := NewTunnelNetwork(net.IPv4(10, 0, 0, 2), 32, []string{"10.0.0.1"}, packetIO)
	if err != nil {
		t.Fatalf("NewTunnelNetwork: %v", err)
	}
	defer network.Close()

	conn, err := network.DialContext(context.Background(), "udp", "10.0.0.1:5060")
	if err != nil {
		t.Fatalf("DialContext: %v", err)
	}
	defer conn.Close()
	payload := []byte("REGISTER sip:ims.example SIP/2.0\r\n\r\n")
	if _, err := conn.Write(payload); err != nil {
		t.Fatalf("Write: %v", err)
	}

	select {
	case packet := <-packetIO.outbound:
		assertIPv4UDPPacket(t, packet, net.IPv4(10, 0, 0, 1), payload)
	case <-time.After(time.Second):
		t.Fatal("UDP packet did not reach SWu packet IO")
	}
}

func assertIPv4UDPPacket(t *testing.T, packet []byte, destination net.IP, payload []byte) {
	t.Helper()
	if len(packet) < 28 || packet[0]>>4 != 4 {
		t.Fatalf("invalid IPv4 packet: %x", packet)
	}
	if got := net.IP(packet[16:20]); !got.Equal(destination) {
		t.Fatalf("destination = %s, want %s", got, destination)
	}
	headerLen := int(packet[0]&0x0f) * 4
	if len(packet) < headerLen+8 || !bytes.Equal(packet[headerLen+8:], payload) {
		t.Fatalf("UDP payload mismatch: %x", packet)
	}
}

func TestNewNetworkWithoutPacketIOFailsExplicitly(t *testing.T) {
	network := NewNetwork(net.IPv4(10, 0, 0, 2), 32, nil)
	if _, err := network.DialContext(context.Background(), "udp", "10.0.0.1:5060"); err == nil {
		t.Fatal("DialContext error=nil, want missing SWu packet IO")
	}
}

func TestPreferIPv4ForIPv4OnlyTunnel(t *testing.T) {
	addresses := []net.IP{net.ParseIP("2001:db8::1"), net.ParseIP("192.0.2.10")}
	if got := preferIPv4(addresses); !got.Equal(net.ParseIP("192.0.2.10")) {
		t.Fatalf("preferIPv4() = %v, want IPv4 address", got)
	}
}
