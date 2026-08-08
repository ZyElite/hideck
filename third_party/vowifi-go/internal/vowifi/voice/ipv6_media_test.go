package voice

import (
	"net"
	"testing"

	"github.com/iniwex5/vowifi-go/internal/vowifi/imscore"
)

type recordingIMSPacketNetwork struct {
	address *net.UDPAddr
}

func (n *recordingIMSPacketNetwork) ListenPacket(_ string, address *net.UDPAddr) (net.PacketConn, error) {
	n.address = address
	return net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
}

func TestAgentLocalIPPreservesBareIPv6(t *testing.T) {
	localIP := net.ParseIP("2a03:dd00:11d1:b5c4:cef3:ff08:546e:1234")
	service, err := imscore.New(&imscore.IMSConfig{LocalIP: localIP})
	if err != nil {
		t.Fatalf("imscore.New: %v", err)
	}
	if got := NewAgent("wwan0", service, nil).localIP(); got != localIP.String() {
		t.Fatalf("localIP()=%q want %q", got, localIP.String())
	}
}

func TestVoiceHostAcceptsBareAndPortQualifiedAddresses(t *testing.T) {
	tests := map[string]string{
		"2a03:dd00:11d1:b5c4:cef3:ff08:546e:1234":        "2a03:dd00:11d1:b5c4:cef3:ff08:546e:1234",
		"[2a03:dd00:11d1:b5c4:cef3:ff08:546e:1234]:5060": "2a03:dd00:11d1:b5c4:cef3:ff08:546e:1234",
		"192.0.2.8":      "192.0.2.8",
		"192.0.2.8:5060": "192.0.2.8",
	}
	for input, want := range tests {
		if got := voiceHost(input); got != want {
			t.Errorf("voiceHost(%q)=%q want %q", input, got, want)
		}
	}
}

func TestVoiceMediaRelayUsesIMSNetworkForIPv6(t *testing.T) {
	network := &recordingIMSPacketNetwork{}
	localIP := net.ParseIP("2001:db8::8")
	relay, err := newVoiceMediaRelay(network, localIP.String())
	if err != nil {
		t.Fatalf("newVoiceMediaRelay: %v", err)
	}
	t.Cleanup(func() { _ = relay.Stop() })
	if network.address == nil || !network.address.IP.Equal(localIP) {
		t.Fatalf("IMS listener address=%v want %s", network.address, localIP)
	}
}

func TestRewriteSDPUsesIPv6AddressFamily(t *testing.T) {
	input := "v=0\r\nc=IN IP4 192.0.2.1\r\nm=audio 10000 RTP/AVP 0\r\n"
	got := RewriteSDP(input, "2001:db8::8", 20000)
	want := "v=0\r\nc=IN IP6 2001:db8::8\r\nm=audio 20000 RTP/AVP 0\r\n"
	if got != want {
		t.Fatalf("RewriteSDP()=%q want %q", got, want)
	}
}

func TestMediaRemoteAcceptsIPv6(t *testing.T) {
	info, err := ParseSDP("v=0\r\nc=IN IP6 2001:db8::9\r\nm=audio 30000 RTP/AVP 0\r\n")
	if err != nil {
		t.Fatalf("ParseSDP: %v", err)
	}
	remote, err := mediaRemote(info)
	if err != nil || !remote.IP.Equal(net.ParseIP("2001:db8::9")) || remote.Port != 30000 {
		t.Fatalf("mediaRemote()=%v err=%v", remote, err)
	}
}
