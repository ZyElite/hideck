package voice

import (
	"context"
	"fmt"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/iniwex5/vowifi-go/internal/vowifi/imscore"
	"github.com/iniwex5/vowifi-go/internal/vowifi/voice/callstate"
)

const reliableEarlyMediaSDP = "v=0\r\no=- 2 2 IN IP4 127.0.0.1\r\ns=ims\r\nc=IN IP4 127.0.0.1\r\nt=0 0\r\nm=audio 33000 RTP/AVP 104\r\na=rtpmap:104 AMR-WB/16000\r\n"

type reliableProvisionalRegistrar struct {
	conn                *net.UDPConn
	prack               chan string
	ack                 chan string
	prackResponsesAfter int
	prackCount          int
	sessionExpires      string
	provisionalExpires  string
}

type sipTestResponse struct {
	request string
	remote  *net.UDPAddr
	status  int
	extra   string
	body    string
}

type reliableRegistrarOptions struct {
	prackResponsesAfter       int
	finalSessionExpires       string
	provisionalSessionExpires string
}

func startReliableProvisionalRegistrar(t *testing.T) *reliableProvisionalRegistrar {
	return startReliableProvisionalRegistrarWithOptions(t, reliableRegistrarOptions{prackResponsesAfter: 1})
}

func startReliableProvisionalRegistrarWithOptions(
	t *testing.T,
	options reliableRegistrarOptions,
) *reliableProvisionalRegistrar {
	t.Helper()
	conn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatalf("ListenUDP: %v", err)
	}
	registrar := &reliableProvisionalRegistrar{
		conn: conn, prack: make(chan string, 4), ack: make(chan string, 1),
		prackResponsesAfter: options.prackResponsesAfter,
		sessionExpires:      options.finalSessionExpires,
		provisionalExpires:  options.provisionalSessionExpires,
	}
	t.Cleanup(func() { _ = conn.Close() })
	go registrar.serve()
	return registrar
}

func (r *reliableProvisionalRegistrar) serve() {
	buffer := make([]byte, 64*1024)
	var invite string
	var inviteRemote *net.UDPAddr
	for {
		n, remote, err := r.conn.ReadFromUDP(buffer)
		if err != nil {
			return
		}
		request := string(buffer[:n])
		switch sipMethodForTest(request) {
		case "INVITE":
			invite, inviteRemote = request, remote
			r.writeProvisional(request, remote)
		case "PRACK":
			r.prack <- request
			r.prackCount++
			if r.prackCount < r.prackResponsesAfter {
				continue
			}
			r.writeResponse(sipTestResponse{request: request, remote: remote, status: 200})
			r.writeFinalInvite(invite, inviteRemote)
		case "ACK":
			r.ack <- request
		default:
			r.writeResponse(sipTestResponse{request: request, remote: remote, status: 200})
		}
	}
}

func (r *reliableProvisionalRegistrar) writeProvisional(request string, remote *net.UDPAddr) {
	contact := fmt.Sprintf("<sip:callee@127.0.0.1:%d>", r.conn.LocalAddr().(*net.UDPAddr).Port)
	extra := "To: <sip:callee@ims.example.com>;tag=early-dialog\r\n" +
		"Contact: " + contact + "\r\n" +
		"Record-Route: <sip:edge-one.example;lr>, <sip:edge-two.example;lr>\r\n" +
		"Require: 100rel\r\nRSeq: 41\r\nContent-Type: application/sdp\r\n"
	if r.provisionalExpires != "" {
		extra += "Session-Expires: " + r.provisionalExpires + "\r\n"
	}
	r.writeResponse(sipTestResponse{
		request: request, remote: remote, status: 183, extra: extra, body: reliableEarlyMediaSDP,
	})
}

func (r *reliableProvisionalRegistrar) writeFinalInvite(request string, remote *net.UDPAddr) {
	if request == "" || remote == nil {
		return
	}
	extra := "To: <sip:callee@ims.example.com>;tag=early-dialog\r\n"
	if r.sessionExpires != "" {
		extra += "Session-Expires: " + r.sessionExpires + "\r\n"
	}
	r.writeResponse(sipTestResponse{request: request, remote: remote, status: 200, extra: extra})
}

func (r *reliableProvisionalRegistrar) writeResponse(cfg sipTestResponse) {
	if sipMethodForTest(cfg.request) == "REGISTER" {
		cfg.extra += "P-Associated-URI: <sip:+15551234567@ims.example.com>\r\n"
	}
	response := fmt.Sprintf("SIP/2.0 %d %s\r\nVia: %s\r\nCall-ID: %s\r\nCSeq: %s\r\n%sContent-Length: %d\r\n\r\n%s",
		cfg.status, imscore.SIPStatusText(cfg.status), voiceTestHeader(cfg.request, "Via"),
		voiceTestHeader(cfg.request, "Call-ID"), voiceTestHeader(cfg.request, "CSeq"), cfg.extra, len(cfg.body), cfg.body)
	_, _ = r.conn.WriteToUDP([]byte(response), cfg.remote)
}

func sipMethodForTest(request string) string {
	method, _, _ := strings.Cut(request, " ")
	return strings.ToUpper(method)
}

func TestAgentPRACKsReliableProvisionalBeforeFinalInvite(t *testing.T) {
	registrar := startReliableProvisionalRegistrar(t)
	agent := newVoiceTestAgent(t, registrar.conn)
	if err := agent.Start(); err != nil {
		t.Fatal(err)
	}
	defer agent.Stop()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	call, err := agent.dialContext(ctx, "+447942985429", testClientSDP)
	if err != nil {
		t.Fatalf("dialContext: %v", err)
	}
	if call.GetState() != callstate.StateConnected || !call.HasReliableProvisional() {
		t.Fatalf("state=%s reliable=%t", call.GetState(), call.HasReliableProvisional())
	}
	if call.sessionTimer != nil {
		t.Fatal("call without Session-Expires installed a session timer")
	}
	inviteCSeq := call.voiceDialogSnapshot().inviteCSeq
	assertReliableProvisionalPRACK(t, <-registrar.prack, registrar.conn.LocalAddr().(*net.UDPAddr).Port, inviteCSeq)
	wantACKCSeq := fmt.Sprintf("%d ACK", inviteCSeq)
	if ack := <-registrar.ack; voiceTestHeader(ack, "CSeq") != wantACKCSeq {
		t.Fatalf("ACK CSeq = %q, want %s", voiceTestHeader(ack, "CSeq"), wantACKCSeq)
	}
}

func TestAgentRetransmitsPRACKWithOriginalTransaction(t *testing.T) {
	registrar := startReliableProvisionalRegistrarWithOptions(t, reliableRegistrarOptions{
		prackResponsesAfter: 2, finalSessionExpires: "120;refresher=uac",
	})
	agent := newVoiceTestAgent(t, registrar.conn)
	if err := agent.Start(); err != nil {
		t.Fatal(err)
	}
	defer agent.Stop()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	call, err := agent.dialContext(ctx, "+447942985429", testClientSDP)
	if err != nil {
		t.Fatalf("dialContext: %v", err)
	}
	first, second := <-registrar.prack, <-registrar.prack
	for _, name := range []string{"Via", "Call-ID", "CSeq", "RAck"} {
		if voiceTestHeader(first, name) != voiceTestHeader(second, name) {
			t.Fatalf("retransmitted PRACK %s changed: %q / %q", name, first, second)
		}
	}
	if call.voiceSessionExpires() != 120*time.Second || call.sessionTimer == nil {
		t.Fatalf("negotiated session timer = %s, timer=%v", call.voiceSessionExpires(), call.sessionTimer)
	}
	if call.prackTimer != nil {
		t.Fatal("PRACK timer remains active after final response")
	}
}

func TestAgentRetainsSessionExpiryFromReliableProvisional(t *testing.T) {
	registrar := startReliableProvisionalRegistrarWithOptions(t, reliableRegistrarOptions{
		prackResponsesAfter: 1, provisionalSessionExpires: "180;refresher=uac",
	})
	agent := newVoiceTestAgent(t, registrar.conn)
	if err := agent.Start(); err != nil {
		t.Fatal(err)
	}
	defer agent.Stop()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	call, err := agent.dialContext(ctx, "+447942985429", testClientSDP)
	if err != nil {
		t.Fatalf("dialContext: %v", err)
	}
	if call.voiceSessionExpires() != 180*time.Second || call.sessionTimer == nil {
		t.Fatalf("provisional session timer = %s, timer=%v", call.voiceSessionExpires(), call.sessionTimer)
	}
}

func assertReliableProvisionalPRACK(t *testing.T, request string, registrarPort, inviteCSeq int) {
	t.Helper()
	wantTarget := fmt.Sprintf("PRACK sip:callee@127.0.0.1:%d SIP/2.0", registrarPort)
	if !strings.HasPrefix(request, wantTarget) {
		t.Fatalf("PRACK target = %q", strings.Split(request, "\r\n")[0])
	}
	if got := voiceTestHeader(request, "RAck"); got != fmt.Sprintf("41 %d INVITE", inviteCSeq) {
		t.Fatalf("RAck = %q", got)
	}
	if got := voiceTestHeader(request, "CSeq"); got != fmt.Sprintf("%d PRACK", inviteCSeq+1) {
		t.Fatalf("PRACK CSeq = %q", got)
	}
	if got := voiceTestHeader(request, "To"); !strings.Contains(got, "tag=early-dialog") {
		t.Fatalf("PRACK To = %q", got)
	}
	first := strings.Index(request, "Route: <sip:edge-two.example;lr>")
	second := strings.Index(request, "Route: <sip:edge-one.example;lr>")
	if first < 0 || second < first {
		t.Fatalf("PRACK route set is not reversed: %q", request)
	}
}
