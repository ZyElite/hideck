package voice

import (
	"context"
	"fmt"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/emiago/sipgo/sip"
	"github.com/iniwex5/vowifi-go/internal/vowifi/events"
	"github.com/iniwex5/vowifi-go/internal/vowifi/imscore"
	"github.com/iniwex5/vowifi-go/internal/vowifi/imsendpoint"
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
	provisionalSDP      string
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
	provisionalSDP            string
}

type earlyMediaProbe struct {
	call        *Call
	imsMedia    *net.UDPConn
	clientMedia *net.UDPConn
}

type recoveredEarlyDialogCallAPI interface {
	StartPrackRuntimeRetransmission(func())
	StopPrackTimer()
}

var _ recoveredEarlyDialogCallAPI = (*Call)(nil)

type recoveredEarlyDialogGatewayAPI interface {
	HandleClientPrack(string, *sip.Request, sip.ServerTransaction)
}

var _ recoveredEarlyDialogGatewayAPI = (*Gateway)(nil)

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
		provisionalSDP:      options.provisionalSDP,
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
	body := r.provisionalSDP
	if body == "" {
		body = reliableEarlyMediaSDP
	}
	r.writeResponse(sipTestResponse{request: request, remote: remote, status: 183, extra: extra, body: body})
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
	if call.CallState() != callstate.StateConnected || !call.HasReliableProvisional() {
		t.Fatalf("state=%s reliable=%t", call.CallState(), call.HasReliableProvisional())
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

func TestLocalClientOwnsReliableProvisionalPRACK(t *testing.T) {
	clientMedia := listenVoiceUDP(t)
	imsMedia := listenVoiceUDP(t)
	registrar := startReliableProvisionalRegistrarWithOptions(t, reliableRegistrarOptions{
		prackResponsesAfter: 1,
		provisionalSDP:      voiceTestSDP("ims", imsMedia.LocalAddr().(*net.UDPAddr).Port, 104),
	})
	agent := newVoiceTestAgent(t, registrar.conn)
	if err := agent.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = agent.Stop() })

	inviteTx := newVoiceServerTransaction()
	clientOffer := voiceTestSDP("client", clientMedia.LocalAddr().(*net.UDPAddr).Port, 104)
	invite := mustClientRequest(t, sip.INVITE, "client-100rel", clientOffer, "")
	agent.HandleOutboundInvite(invite, inviteTx)
	provisional := waitVoiceResponse(t, inviteTx, 183)
	if got := requestHeaderValueFromResponse(provisional, "Require"); got != "100rel" {
		t.Fatalf("forwarded Require = %q", got)
	}
	if got := requestHeaderValueFromResponse(provisional, "RSeq"); got != "41" {
		t.Fatalf("forwarded RSeq = %q", got)
	}
	if !isVoiceSDPContentType(requestHeaderValueFromResponse(provisional, "Content-Type")) ||
		len(provisional.Body()) == 0 {
		t.Fatalf("forwarded early media = %q", provisional.Body())
	}
	select {
	case request := <-registrar.prack:
		t.Fatalf("agent sent PRACK before the local client: %q", request)
	default:
	}
	assertEarlyMediaRelayed(t, earlyMediaProbe{
		call: agent.ActiveCall(), imsMedia: imsMedia, clientMedia: clientMedia,
	})

	prackTx := newVoiceServerTransaction()
	prack := mustClientRequest(
		t, sip.PRACK, "client-100rel", "", "RAck: 41 1 INVITE\r\n",
	)
	NewGateway(agent).HandleClientPrack(agent.DeviceID(), prack, prackTx)
	waitVoiceResponse(t, prackTx, 200)
	select {
	case request := <-registrar.prack:
		if got := voiceTestHeader(request, "RAck"); got != "41 1 INVITE" {
			t.Fatalf("IMS RAck = %q", got)
		}
	case <-time.After(time.Second):
		t.Fatal("local PRACK was not forwarded to IMS")
	}
	waitVoiceResponse(t, inviteTx, 200)
}

func voiceTestSDP(name string, port, payloadType int) string {
	return fmt.Sprintf(
		"v=0\r\no=- 2 2 IN IP4 127.0.0.1\r\ns=%s\r\nc=IN IP4 127.0.0.1\r\n"+
			"t=0 0\r\nm=audio %d RTP/AVP %d\r\na=rtpmap:%d AMR-WB/16000\r\n",
		name, port, payloadType, payloadType,
	)
}

func assertEarlyMediaRelayed(t *testing.T, probe earlyMediaProbe) {
	t.Helper()
	if probe.call == nil || probe.call.RTPRelay() == nil {
		t.Fatal("early-media RTP relay is unavailable")
	}
	packet := []byte{0x80, 0x68, 0, 1, 0, 0, 0, 1, 0, 0, 0, 2, 0x55}
	destination := &net.UDPAddr{
		IP: net.IPv4(127, 0, 0, 1), Port: probe.call.RTPRelay().IMSPort(),
	}
	if _, err := probe.imsMedia.WriteToUDP(packet, destination); err != nil {
		t.Fatal(err)
	}
	if err := probe.clientMedia.SetReadDeadline(time.Now().Add(500 * time.Millisecond)); err != nil {
		t.Fatal(err)
	}
	received := make([]byte, len(packet))
	n, _, err := probe.clientMedia.ReadFromUDP(received)
	if err != nil {
		t.Fatalf("early media was not relayed before final response: %v", err)
	}
	if string(received[:n]) != string(packet) {
		t.Fatalf("relayed early media = %x, want %x", received[:n], packet)
	}
}

func requestHeaderValueFromResponse(response *sip.Response, name string) string {
	if response == nil {
		return ""
	}
	header := response.GetHeader(name)
	if header == nil {
		return ""
	}
	return strings.TrimSpace(header.Value())
}

func TestAgentEmitsRingingAfterRecoveredProvisionalTransition(t *testing.T) {
	agent := NewAgent("device-ringing", nil, nil)
	call := NewCall(agent, callstate.DirectionOutbound, "call-ringing", "43430")
	t.Cleanup(call.Cancel)
	if err := call.TransitionChecked(callstate.StateDialing); err != nil {
		t.Fatal(err)
	}
	agent.mu.Lock()
	agent.calls[call.CallID()] = call
	agent.mu.Unlock()
	ringing := make(chan events.Event, 1)
	agent.SetNotifier(func(event events.Event) { ringing <- event })
	if err := agent.handleOutboundProvisional(context.Background(), call, imscore.SIPResponse{
		StatusCode: 180,
		Reason:     "Ringing",
	}); err != nil {
		t.Fatal(err)
	}
	if call.CallState() != callstate.StateAlerting {
		t.Fatalf("state=%s, want Alerting", call.CallState())
	}
	select {
	case event := <-ringing:
		if event.Type() != "CallRinging" {
			t.Fatalf("event=%s, want CallRinging", event.Type())
		}
	case <-time.After(time.Second):
		t.Fatal("180 response did not emit ringing event")
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

func TestPRACKResponseEventStopsCompatibilityTimer(t *testing.T) {
	agent := NewAgent("device-prack-event", nil, nil)
	call := NewCall(agent, callstate.DirectionOutbound, "prack-event", "43430")
	t.Cleanup(call.Cancel)
	agent.mu.Lock()
	agent.calls[call.CallID()] = call
	agent.activeCall = call
	agent.mu.Unlock()
	call.StartPrackRuntimeRetransmission(func() {})
	response := sip.NewResponse(200, "OK")
	response.AppendHeader(sip.NewHeader("Call-ID", call.CallID()))
	response.AppendHeader(&sip.CSeqHeader{SeqNo: 2, MethodName: sip.PRACK})
	agent.handleIMSEvent(imsendpoint.Event{
		Kind: "response", CallID: call.CallID(), CSeqMethod: "PRACK", Response: response,
	})
	call.mu.RLock()
	timer, retry := call.prackTimer, call.prackRetransmit
	call.mu.RUnlock()
	if timer != nil || retry != nil {
		t.Fatalf("PRACK runtime remains active: timer=%v retry=%v", timer, retry != nil)
	}
}

func TestPrackRuntimeRetransmissionStopsBeforeNextBackoff(t *testing.T) {
	call := NewCall(nil, callstate.DirectionOutbound, "prack-timer", "43430")
	t.Cleanup(call.Cancel)
	retries := make(chan struct{}, 2)
	call.startPrackRuntimeRetransmission(time.Millisecond, func() { retries <- struct{}{} })
	select {
	case <-retries:
	case <-time.After(100 * time.Millisecond):
		t.Fatal("PRACK retry did not run")
	}
	call.StopPrackTimer()
	select {
	case <-retries:
		t.Fatal("PRACK retry ran after StopPrackTimer")
	case <-time.After(5 * time.Millisecond):
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
