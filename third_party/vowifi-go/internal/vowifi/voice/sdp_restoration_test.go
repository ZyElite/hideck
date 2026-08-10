package voice

import (
	"net"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/iniwex5/vowifi-go/internal/vowifi/voice/callstate"
	"github.com/iniwex5/vowifi-go/internal/vowifi/voice/media"
)

var (
	_ func([]byte) (*SDPInfo, error)                          = ParseSDP
	_ func([]byte, string, int) []byte                        = RewriteSDP
	_ func([]byte, string, int, []byte) ([]byte, map[int]int) = RewriteSDPForClient
	_ func(*Call, []byte, string) ([]byte, error)             = ProcessIncomingIMSSDP
	_ func(*Call, []byte, string) ([]byte, error)             = ProcessOutgoingClientSDP
	_ func(*Call, []byte)                                     = ExtractAndApplyPTMapping
	_ func(*SDPInfo, string, int, int) *CodecInfo             = (*SDPInfo).FindCodec
	_ func(*SDPInfo, int) *CodecInfo                          = (*SDPInfo).GetCodecByPT
	_ func(*SDPInfo) *CodecInfo                               = (*SDPInfo).GetPreferredCodec
	_ func(*SDPInfo) string                                   = (*SDPInfo).GetMediaAddress
	_ func(*Agent) []byte                                     = (*Agent).generateBasicSDP
)

func TestRecoveredSDPTypeLayouts(t *testing.T) {
	assertStructFields(t, reflect.TypeOf(SDPInfo{}), []structFieldExpectation{
		{"ConnectionIP", reflect.TypeOf("")},
		{"MediaPort", reflect.TypeOf(0)},
		{"MediaType", reflect.TypeOf("")},
		{"Codecs", reflect.TypeOf([]CodecInfo{})},
		{"RawSDP", reflect.TypeOf([]byte{})},
	})
	assertStructFields(t, reflect.TypeOf(CodecInfo{}), []structFieldExpectation{
		{"PayloadType", reflect.TypeOf(0)},
		{"Name", reflect.TypeOf("")},
		{"ClockRate", reflect.TypeOf(0)},
		{"Channels", reflect.TypeOf(0)},
		{"Fmtp", reflect.TypeOf("")},
	})
}

type structFieldExpectation struct {
	name   string
	typeOf reflect.Type
}

func assertStructFields(t *testing.T, actual reflect.Type, expected []structFieldExpectation) {
	t.Helper()
	if actual.NumField() != len(expected) {
		t.Fatalf("%s field count=%d want %d", actual.Name(), actual.NumField(), len(expected))
	}
	for index, want := range expected {
		field := actual.Field(index)
		if field.Name != want.name || field.Type != want.typeOf {
			t.Fatalf("%s field[%d]=%s %s want %s %s", actual.Name(), index,
				field.Name, field.Type, want.name, want.typeOf)
		}
	}
}

func TestParseSDPRecoveredBehavior(t *testing.T) {
	if info, err := ParseSDP(nil); info != nil || err != nil {
		t.Fatalf("ParseSDP(nil)=(%v,%v), want (nil,nil)", info, err)
	}
	raw := []byte("bad\r\nc=IN IP6 2001:db8::9\r\nm=audio 32100 RTP/AVP 110\r\n" +
		"a=rtpmap:110 opus/48000/2\r\na=fmtp:\t110\tminptime=10; useinbandfec=1\r\n")
	info, err := ParseSDP(raw)
	if err != nil {
		t.Fatal(err)
	}
	codec := info.FindCodec("OPUS", 48000, 2)
	if info.MediaType != "audio" || info.GetMediaAddress() != "[2001:db8::9]:32100" ||
		codec == nil || codec.Fmtp != "minptime=10; useinbandfec=1" {
		t.Fatalf("parsed SDP=%+v codec=%+v", info, codec)
	}
	if info.GetCodecByPT(110) != codec || info.GetPreferredCodec() != codec ||
		info.FindCodec("opus", 0, 0) != codec {
		t.Fatal("recovered codec lookup behavior is incomplete")
	}
	raw[0] = 'B'
	if info.RawSDP[0] != 'B' {
		t.Fatal("RawSDP no longer aliases the parser input")
	}
}

func TestDisplacedCurrentSDPModelRemainsAvailable(t *testing.T) {
	raw := "v=0\r\no=- 1 1 IN IP4 127.0.0.1\r\ns=current\r\nc=IN IP4 127.0.0.1\r\n" +
		"m=audio 32000 RTP/AVP 96\r\na=rtpmap:96 opus/48000/2\r\na=fmtp:96 useinbandfec=1\r\n"
	info, err := ParseSDPCurrent(raw)
	if err != nil {
		t.Fatal(err)
	}
	codec := info.FindCodec(96)
	if info.Origin == "" || info.GetMediaAddress() != "127.0.0.1" ||
		info.GetMediaPort() != 32000 || codec == nil || codec.Encoding != "opus" ||
		codec.Fmtp != "useinbandfec=1" {
		t.Fatalf("current SDP model=%+v codec=%+v", info, codec)
	}
	rewritten := RewriteSDPCurrent(raw+"a=crypto:1 retained\r\n", "192.0.2.4", 42000)
	if !strings.Contains(rewritten, "o=- 1 1 IN IP4 127.0.0.1") ||
		!strings.Contains(rewritten, "m=audio 42000 RTP/AVP 96") ||
		!strings.Contains(rewritten, "a=crypto:1 retained") ||
		!strings.HasSuffix(rewritten, "\r\n") {
		t.Fatalf("current string rewrite behavior changed: %q", rewritten)
	}
}

func TestRewriteSDPRecoveredBehavior(t *testing.T) {
	raw := []byte("v=0\r\no=- 1 1 IN IP4 192.0.2.1\r\nc=IN IP4 192.0.2.1\r\n" +
		"m=audio 10000 RTP/SAVP 110\r\na=crypto:1 AES_CM_128_HMAC_SHA1_80 inline:key\r\n" +
		"a=rtcp:10001 IN IP4 192.0.2.1\r\n")
	got := string(RewriteSDP(raw, "2001:db8::8", 22000))
	for _, want := range []string{
		"o=- 1 1 IN IP6 2001:db8::8", "c=IN IP6 2001:db8::8",
		"m=audio 22000 RTP/AVP 110", "a=rtcp:22001",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("rewritten SDP missing %q: %q", want, got)
		}
	}
	if strings.Contains(got, "a=crypto:") || strings.HasSuffix(got, "\r\n") {
		t.Fatalf("rewritten SDP crypto/trailer mismatch: %q", got)
	}
}

func TestRewriteSDPForClientMapsPayloadZeroAndTelephoneEvent(t *testing.T) {
	ims := []byte("v=0\r\nc=IN IP4 198.51.100.8\r\nm=audio 33000 RTP/AVP 110\r\n" +
		"a=rtpmap:110 PCMU/8000\r\n")
	client := []byte("v=0\r\nc=IN IP4 127.0.0.1\r\nm=audio 32000 RTP/SAVP 0 101\r\n" +
		"a=rtpmap:0 PCMU/8000\r\na=rtpmap:101 telephone-event/8000\r\n" +
		"a=crypto:1 AES_CM_128_HMAC_SHA1_80 inline:client-key\r\n")
	rewritten, mapping := RewriteSDPForClient(ims, "127.0.0.1", 24000, client)
	got := string(rewritten)
	if mapping[110] != 0 || !strings.Contains(got, "m=audio 24000 RTP/SAVP 0 101") ||
		!strings.Contains(got, "a=rtpmap:0 PCMU/8000") ||
		!strings.Contains(got, "a=fmtp:101 0-16") ||
		!strings.Contains(got, "inline:client-key") {
		t.Fatalf("rewritten=%q mapping=%v", got, mapping)
	}
}

func TestRecoveredSDPFlowErrorSurface(t *testing.T) {
	if _, err := ProcessIncomingIMSSDP(nil, []byte("v=0"), "127.0.0.1"); err == nil || err.Error() != "call 为空" {
		t.Fatalf("nil call error=%v", err)
	}
	call := NewCall(nil, callstate.DirectionOutbound, "sdp-errors", "43430")
	t.Cleanup(func() { call.Cancel(); call.CloseDone() })
	if _, err := ProcessOutgoingClientSDP(call, []byte("v=0"), "127.0.0.1"); err == nil || err.Error() != "RTPRelay 为空，无法处理 SDP" {
		t.Fatalf("nil relay error=%v", err)
	}
	relay := media.NewRTPRelay(listenVoiceMediaUDP(t), listenVoiceMediaUDP(t))
	t.Cleanup(func() { _ = relay.Stop() })
	call.SetRTPRelay(relay)
	if _, err := ProcessOutgoingClientSDP(call, nil, "127.0.0.1"); err == nil || err.Error() != "Client SDP body 为空" {
		t.Fatalf("empty client SDP error=%v", err)
	}
	if rewritten, err := ProcessOutgoingClientSDP(call, []byte("v=0"), "127.0.0.1"); err != nil || string(rewritten) != "v=0" {
		t.Fatalf("permissive nonempty client SDP=%q err=%v", rewritten, err)
	}
}

func TestRecoveredSDPFlowDrivesRelayAndPayloadMapping(t *testing.T) {
	imsRelay := listenVoiceMediaUDP(t)
	lanRelay := listenVoiceMediaUDP(t)
	imsPeer := listenVoiceMediaUDP(t)
	clientPeer := listenVoiceMediaUDP(t)
	relay := media.NewRTPRelay(imsRelay, lanRelay)
	t.Cleanup(func() { _ = relay.Stop() })
	call := NewCall(NewAgent("sdp-flow", nil, nil), callstate.DirectionOutbound, "sdp-call", "43430")
	t.Cleanup(func() { call.Cancel(); call.CloseDone() })
	call.SetRTPRelay(relay)
	client := mediaSDP(clientPeer, 0, "PCMU", 101)
	setCallClientSDP(call, client)
	if _, err := ProcessOutgoingClientSDP(call, client, "127.0.0.1"); err != nil {
		t.Fatal(err)
	}
	ims := mediaSDP(imsPeer, 110, "PCMU", 97)
	rewritten, err := ProcessIncomingIMSSDP(call, ims, "127.0.0.1")
	if err != nil || !strings.Contains(string(rewritten), "m=audio "+strconv.Itoa(relay.LANPort())) {
		t.Fatalf("ProcessIncomingIMSSDP=%q err=%v", rewritten, err)
	}
	packet := []byte{0x80, 110, 0, 1, 0, 0, 0, 1, 0, 0, 0, 1, 0xff}
	if _, err := imsPeer.WriteToUDP(packet, imsRelay.LocalAddr().(*net.UDPAddr)); err != nil {
		t.Fatal(err)
	}
	if err := clientPeer.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	received := make([]byte, 64)
	n, _, err := clientPeer.ReadFromUDP(received)
	if err != nil || n != len(packet) || received[1]&0x7f != 0 {
		t.Fatalf("mapped RTP=%x err=%v", received[:n], err)
	}
}

func mediaSDP(conn *net.UDPConn, audioPT int, audioName string, telephonePT int) []byte {
	port := conn.LocalAddr().(*net.UDPAddr).Port
	return []byte("v=0\r\nc=IN IP4 127.0.0.1\r\nm=audio " + strconv.Itoa(port) +
		" RTP/AVP " + strconv.Itoa(audioPT) + " " + strconv.Itoa(telephonePT) + "\r\n" +
		"a=rtpmap:" + strconv.Itoa(audioPT) + " " + audioName + "/8000\r\n" +
		"a=rtpmap:" + strconv.Itoa(telephonePT) + " telephone-event/8000\r\n")
}

func TestGenerateBasicSDPUsesRecoveredPortSelection(t *testing.T) {
	agent := NewAgent("sdp-basic", nil, nil)
	gateway := NewGateway(agent)
	gateway.SetClientAdapter(&agentVoiceClientAdapter{})
	got := string(agent.generateBasicSDP())
	if !strings.Contains(got, "m=audio 19998 RTP/AVP 104 114 9 8 0 101") {
		t.Fatalf("basic SDP uses wrong payload order or port: %q", got)
	}
	for _, line := range strings.Split(got, "\r\n") {
		fields := strings.Fields(line)
		if len(fields) >= 4 && fields[0] == "o=-" && fields[1] == fields[2] {
			return
		}
	}
	t.Fatal("basic SDP origin does not repeat its Unix session ID")
}
