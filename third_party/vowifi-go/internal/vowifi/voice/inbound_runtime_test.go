package voice

import (
	"fmt"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/iniwex5/vowifi-go/internal/vowifi/imscore"
	"github.com/iniwex5/vowifi-go/internal/vowifi/voice/callstate"
)

type capturedVoiceResponder struct {
	mu        sync.Mutex
	responses []imscore.InboundVoiceResponse
	localTag  string
}

type blockingFinalVoiceResponder struct {
	capturedVoiceResponder
	entered chan struct{}
	release chan struct{}
	once    sync.Once
}

func (r *blockingFinalVoiceResponder) Respond(response imscore.InboundVoiceResponse) error {
	if err := r.capturedVoiceResponder.Respond(response); err != nil {
		return err
	}
	if response.StatusCode >= 200 {
		r.once.Do(func() { close(r.entered) })
		<-r.release
	}
	return nil
}

func (r *capturedVoiceResponder) Respond(response imscore.InboundVoiceResponse) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	response.Body = append([]byte(nil), response.Body...)
	r.responses = append(r.responses, response)
	return nil
}

func (r *capturedVoiceResponder) LocalTag() string { return r.localTag }

func (r *capturedVoiceResponder) statuses() []int {
	r.mu.Lock()
	defer r.mu.Unlock()
	result := make([]int, 0, len(r.responses))
	for _, response := range r.responses {
		result = append(result, response.StatusCode)
	}
	return result
}

func (r *capturedVoiceResponder) lastResponse() imscore.InboundVoiceResponse {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.responses) == 0 {
		return imscore.InboundVoiceResponse{}
	}
	return r.responses[len(r.responses)-1]
}

func TestInboundCallAnswerRelaysRTPAndRemoteBYECleansUp(t *testing.T) {
	agent := startedVoiceAgent(t)
	imsPeer := listenVoiceUDP(t)
	responder := &capturedVoiceResponder{localTag: "local-tag"}
	var delivered IncomingCall
	agent.SetIncomingCallHandler(func(call IncomingCall) { delivered = call })
	request := inboundAgentInvite("call-in-answer", imsPeer, responder)
	result, err := agent.HandleInboundVoiceRequest(request)
	if err != nil || result.StatusCode != 0 {
		t.Fatalf("HandleInboundVoiceRequest result=%+v err=%v", result, err)
	}
	if got := responder.statuses(); fmt.Sprint(got) != "[180]" {
		t.Fatalf("provisional statuses = %v", got)
	}
	offer, err := ParseSDP(delivered.OfferSDP)
	if err != nil || offer.GetMediaPort() <= 0 || offer.GetMediaPort() == imsPeer.LocalAddr().(*net.UDPAddr).Port {
		t.Fatalf("delivered offer = %q err=%v", delivered.OfferSDP, err)
	}
	client := listenVoiceUDP(t)
	answer, err := agent.AnswerWithSDP(delivered.CallID, voiceSDP(client.LocalAddr().(*net.UDPAddr).Port))
	if err != nil || answer.State != callstate.StateConnected.String() {
		t.Fatalf("AnswerWithSDP answer=%+v err=%v", answer, err)
	}
	call := agent.callByID(delivered.CallID)
	assertVoiceRelayPacket(t, client, offer.GetMediaPort(), imsPeer, call.RTPRelay().IMSPort())
	if _, err := agent.HandleInboundVoiceRequest(imscore.InboundVoiceRequest{Method: "BYE", CallID: call.CallID()}); err != nil {
		t.Fatal(err)
	}
	if call.GetState() != callstate.StateEnded || agent.IsBusy() {
		t.Fatalf("BYE cleanup state=%s busy=%t", call.GetState(), agent.IsBusy())
	}
	conn, _ := call.RTPRelay().GetIMSConnAndRemote()
	if _, err := conn.WriteTo([]byte("closed"), imsPeer.LocalAddr()); err == nil {
		t.Fatal("RTP socket remained writable after BYE")
	}
}

func TestInboundCallRejectAndCancelSendFinalInviteResponses(t *testing.T) {
	for _, tc := range []struct {
		name       string
		finish     func(*Agent, string) error
		wantStatus int
	}{
		{name: "reject", wantStatus: 603, finish: func(agent *Agent, callID string) error {
			return agent.Reject(callID, 603)
		}},
		{name: "cancel", wantStatus: 487, finish: func(agent *Agent, callID string) error {
			cancelResponder := &capturedVoiceResponder{localTag: "cancel-tag"}
			result, err := agent.HandleInboundVoiceRequest(imscore.InboundVoiceRequest{
				Method: "CANCEL", CallID: callID, Responder: cancelResponder,
			})
			if err == nil && (result.StatusCode != 0 || fmt.Sprint(cancelResponder.statuses()) != "[200]" || cancelResponder.lastResponse().ToTag != "local-tag") {
				return fmt.Errorf("CANCEL status = %d", result.StatusCode)
			}
			return err
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			agent := startedVoiceAgent(t)
			peer := listenVoiceUDP(t)
			responder := &capturedVoiceResponder{localTag: "local-tag"}
			request := inboundAgentInvite("call-in-"+tc.name, peer, responder)
			if _, err := agent.HandleInboundVoiceRequest(request); err != nil {
				t.Fatal(err)
			}
			if err := tc.finish(agent, request.CallID); err != nil {
				t.Fatal(err)
			}
			if got := responder.statuses(); fmt.Sprint(got) != fmt.Sprintf("[180 %d]", tc.wantStatus) {
				t.Fatalf("responses = %v", got)
			}
			if agent.IsBusy() {
				t.Fatal("finished inbound call remained active")
			}
			if calls := agent.IncomingCalls(); len(calls) != 0 {
				t.Fatalf("finished inbound call remained pollable: %+v", calls)
			}
		})
	}
}

func TestInboundBYEWaitsForConcurrentAnswerDecision(t *testing.T) {
	agent := startedVoiceAgent(t)
	peer := listenVoiceUDP(t)
	responder := &blockingFinalVoiceResponder{
		capturedVoiceResponder: capturedVoiceResponder{localTag: "local-tag"},
		entered:                make(chan struct{}),
		release:                make(chan struct{}),
	}
	request := inboundAgentInvite("call-in-answer-bye", peer, responder)
	if _, err := agent.HandleInboundVoiceRequest(request); err != nil {
		t.Fatal(err)
	}
	client := listenVoiceUDP(t)
	answerDone := make(chan error, 1)
	go func() {
		_, err := agent.AnswerWithSDP(request.CallID, voiceSDP(client.LocalAddr().(*net.UDPAddr).Port))
		answerDone <- err
	}()
	<-responder.entered
	type byeResult struct {
		result imscore.InboundVoiceResult
		err    error
	}
	byeStarted := make(chan struct{})
	byeDone := make(chan byeResult, 1)
	go func() {
		close(byeStarted)
		result, err := agent.HandleInboundVoiceRequest(imscore.InboundVoiceRequest{
			Method: "BYE", CallID: request.CallID,
		})
		byeDone <- byeResult{result: result, err: err}
	}()
	<-byeStarted
	select {
	case result := <-byeDone:
		t.Fatalf("BYE completed before answer decision: result=%+v err=%v", result.result, result.err)
	case <-time.After(100 * time.Millisecond):
	}
	close(responder.release)
	if err := <-answerDone; err != nil {
		t.Fatal(err)
	}
	result := <-byeDone
	if result.err != nil || result.result.StatusCode != 200 {
		t.Fatalf("BYE result=%+v err=%v", result.result, result.err)
	}
	call := agent.callByID(request.CallID)
	if call.GetState() != callstate.StateEnded || agent.IsBusy() {
		t.Fatalf("final state=%s busy=%t", call.GetState(), agent.IsBusy())
	}
}

func TestInboundReinviteMovesRTPToNewIMSRemote(t *testing.T) {
	agent := startedVoiceAgent(t)
	firstPeer := listenVoiceUDP(t)
	initialResponder := &capturedVoiceResponder{localTag: "local-tag"}
	request := inboundAgentInvite("call-in-reinvite", firstPeer, initialResponder)
	if _, err := agent.HandleInboundVoiceRequest(request); err != nil {
		t.Fatal(err)
	}
	client := listenVoiceUDP(t)
	if _, err := agent.AnswerWithSDP(request.CallID, voiceSDP(client.LocalAddr().(*net.UDPAddr).Port)); err != nil {
		t.Fatal(err)
	}
	secondPeer := listenVoiceUDP(t)
	reinviteResponder := &capturedVoiceResponder{localTag: "local-tag"}
	reinvite := inboundAgentInvite(request.CallID, secondPeer, reinviteResponder)
	result, err := agent.HandleInboundVoiceRequest(reinvite)
	if err != nil || result.StatusCode != 0 || fmt.Sprint(reinviteResponder.statuses()) != "[200]" {
		t.Fatalf("re-INVITE result=%+v statuses=%v err=%v", result, reinviteResponder.statuses(), err)
	}
	call := agent.callByID(request.CallID)
	clientOffer, _ := ParseSDP(call.ClientSDP())
	writeVoicePacket(t, client, clientOffer.GetMediaPort(), []byte("new-ims"))
	if got := readVoicePacket(t, secondPeer); string(got) != "new-ims" {
		t.Fatalf("new IMS peer received %q", got)
	}
}

func startedVoiceAgent(t *testing.T) *Agent {
	t.Helper()
	agent := newTestAgent(t)
	if err := agent.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = agent.Stop() })
	return agent
}

func inboundAgentInvite(callID string, peer *net.UDPConn, responder imscore.InboundVoiceResponder) imscore.InboundVoiceRequest {
	return imscore.InboundVoiceRequest{
		Method: "INVITE", CallID: callID, From: "<sip:+447700900123@ims.example>;tag=remote",
		To: "<sip:user@ims.example>", Contact: "<sip:peer@127.0.0.1>", CSeq: "1 INVITE",
		ContentType: "application/sdp", Body: []byte(voiceSDP(peer.LocalAddr().(*net.UDPAddr).Port)),
		Responder: responder,
	}
}

func voiceSDP(port int) string {
	return fmt.Sprintf("v=0\r\no=- 1 1 IN IP4 127.0.0.1\r\ns=voice\r\nc=IN IP4 127.0.0.1\r\nt=0 0\r\nm=audio %d RTP/AVP 0\r\n", port)
}

func listenVoiceUDP(t *testing.T) *net.UDPConn {
	t.Helper()
	conn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return conn
}

func assertVoiceRelayPacket(t *testing.T, client *net.UDPConn, lanPort int, imsPeer *net.UDPConn, imsPort int) {
	t.Helper()
	writeVoicePacket(t, client, lanPort, []byte("lan-to-ims"))
	if got := readVoicePacket(t, imsPeer); string(got) != "lan-to-ims" {
		t.Fatalf("IMS received %q", got)
	}
	writeVoicePacket(t, imsPeer, imsPort, []byte("ims-to-lan"))
	if got := readVoicePacket(t, client); string(got) != "ims-to-lan" {
		t.Fatalf("client received %q", got)
	}
}

func writeVoicePacket(t *testing.T, conn *net.UDPConn, port int, payload []byte) {
	t.Helper()
	if _, err := conn.WriteToUDP(payload, &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: port}); err != nil {
		t.Fatal(err)
	}
}

func readVoicePacket(t *testing.T, conn *net.UDPConn) []byte {
	t.Helper()
	if err := conn.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	buffer := make([]byte, 256)
	n, _, err := conn.ReadFromUDP(buffer)
	if err != nil {
		t.Fatal(err)
	}
	return buffer[:n]
}

func assertNoZeroMediaPort(t *testing.T, sdp string) {
	t.Helper()
	if strings.Contains(sdp, "m=audio 0 ") {
		t.Fatalf("SDP exposed zero media port: %q", sdp)
	}
}
