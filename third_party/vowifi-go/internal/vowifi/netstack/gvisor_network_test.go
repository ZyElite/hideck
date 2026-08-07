package netstack

import (
	"bytes"
	"context"
	"encoding/binary"
	"net"
	"testing"
	"time"

	"github.com/iniwex5/vowifi-go/internal/vowifi/ipsec3gpp"
	"gvisor.dev/gvisor/pkg/tcpip/network/ipv4"
	"gvisor.dev/gvisor/pkg/tcpip/network/ipv6"
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

func TestTunnelNetworkSendsIPv6UDPThroughPacketIO(t *testing.T) {
	packetIO := newChannelPacketIO()
	local := net.ParseIP("2001:db8::2")
	destination := net.ParseIP("2001:db8::1")
	network, err := NewTunnelNetwork(local, 64, []string{"2001:db8::53"}, packetIO)
	if err != nil {
		t.Fatalf("NewTunnelNetwork: %v", err)
	}
	defer network.Close()

	conn, err := network.DialContext(context.Background(), "udp", "[2001:db8::1]:5060")
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
		assertIPv6UDPPacket(t, packet, destination, payload)
	case <-time.After(time.Second):
		t.Fatal("IPv6 UDP packet did not reach SWu packet IO")
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

func assertIPv6UDPPacket(t *testing.T, packet []byte, destination net.IP, payload []byte) {
	t.Helper()
	const ipv6HeaderLength = 40
	if len(packet) < ipv6HeaderLength+8 || packet[0]>>4 != 6 {
		t.Fatalf("invalid IPv6 packet: %x", packet)
	}
	if got := net.IP(packet[24:40]); !got.Equal(destination) {
		t.Fatalf("destination = %s, want %s", got, destination)
	}
	if !bytes.Equal(packet[ipv6HeaderLength+8:], payload) {
		t.Fatalf("UDP payload mismatch: %x", packet)
	}
}

func TestNewNetworkWithoutPacketIOFailsExplicitly(t *testing.T) {
	network := NewNetwork(net.IPv4(10, 0, 0, 2), 32, nil)
	if _, err := network.DialContext(context.Background(), "udp", "10.0.0.1:5060"); err == nil {
		t.Fatal("DialContext error=nil, want missing SWu packet IO")
	}
}

func TestTunnelNetworkInstallsIPSec3GPPTransformer(t *testing.T) {
	packetIO := newChannelPacketIO()
	network, err := NewTunnelNetwork(net.IPv4(10, 0, 0, 2), 32, nil, packetIO)
	if err != nil {
		t.Fatalf("NewTunnelNetwork: %v", err)
	}
	defer network.Close()
	if network.IPSec3GPPPolicyInstalled() {
		t.Fatal("IPsec policy reported installed before installation")
	}
	policy := ipsec3gpp.Policy{
		LocalIP: net.IPv4(10, 0, 0, 2), RemoteIP: net.IPv4(10, 0, 0, 1),
		LocalClientPort: 41000, LocalServerPort: 41001,
		RemoteClientPort: 51000, RemoteServerPort: 51001,
		LocalClientSPI: 0x11111111, LocalServerSPI: 0x22222222,
		RemoteClientSPI: 0x33333333, RemoteServerSPI: 0x44444444,
		Authentication: ipsec3gpp.AuthHMACSHA196, Encryption: ipsec3gpp.EncryptionAES,
		Protocol: ipsec3gpp.ProtocolESP, Mode: ipsec3gpp.ModeTransport,
		CK: bytes.Repeat([]byte{0x11}, 16), IK: bytes.Repeat([]byte{0x22}, 16),
	}
	if err := network.InstallIPSec3GPP(policy); err != nil {
		t.Fatalf("InstallIPSec3GPP: %v", err)
	}
	if !network.IPSec3GPPPolicyInstalled() {
		t.Fatal("IPsec policy was not marked installed")
	}
	conn, err := network.ListenPacket("udp", &net.UDPAddr{IP: policy.LocalIP, Port: int(policy.LocalClientPort)})
	if err != nil {
		t.Fatalf("ListenPacket: %v", err)
	}
	defer conn.Close()
	if _, err := conn.WriteTo([]byte("REGISTER"), &net.UDPAddr{IP: policy.RemoteIP, Port: int(policy.RemoteServerPort)}); err != nil {
		t.Fatalf("WriteTo: %v", err)
	}
	select {
	case packet := <-packetIO.outbound:
		if len(packet) < 28 || packet[9] != 50 || binary.BigEndian.Uint32(packet[20:24]) != policy.RemoteServerSPI {
			t.Fatalf("outbound packet was not ESP protected: %x", packet)
		}
	case <-time.After(time.Second):
		t.Fatal("protected packet did not reach SWu packet IO")
	}
}

func TestAddressForTunnelMatchesNegotiatedFamily(t *testing.T) {
	addresses := []net.IP{net.ParseIP("2001:db8::1"), net.ParseIP("192.0.2.10")}
	ipv4Network := &gvisorNetwork{protocol: ipv4.ProtocolNumber}
	if got, err := ipv4Network.addressForTunnel(addresses, "pcscf"); err != nil || !got.Equal(net.ParseIP("192.0.2.10")) {
		t.Fatalf("IPv4 addressForTunnel() = %v, %v", got, err)
	}
	ipv6Network := &gvisorNetwork{protocol: ipv6.ProtocolNumber}
	if got, err := ipv6Network.addressForTunnel(addresses, "pcscf"); err != nil || !got.Equal(net.ParseIP("2001:db8::1")) {
		t.Fatalf("IPv6 addressForTunnel() = %v, %v", got, err)
	}
}
