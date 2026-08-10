package voice

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/emiago/sipgo/sip"
	"github.com/iniwex5/vowifi-go/internal/vowifi/imscore"
	"github.com/iniwex5/vowifi-go/internal/vowifi/imsendpoint"
	"github.com/iniwex5/vowifi-go/internal/vowifi/voice/callstate"
)

type failingDialogEndpoint struct {
	imsendpoint.ClientDialogEndpoint
	err error
}

func (e failingDialogEndpoint) SendDialogRequest(
	context.Context,
	string,
	imsendpoint.DialogHandle,
	*sip.Request,
	imsendpoint.DialogRequestOptions,
) (*sip.Response, error) {
	return nil, e.err
}

func installDialogFailure(agent *Agent, message string) {
	agent.dialog.SetEndpoint(failingDialogEndpoint{
		ClientDialogEndpoint: agent.ims,
		err:                  errors.New(message),
	})
}

func TestAgentPreparesCallsThroughDialogControllerContext(t *testing.T) {
	agent := newTestAgent(t)
	if agent.dialog == nil {
		t.Fatal("NewAgent did not bind the dialog controller")
	}
	ctx := agent.dialog.Context()
	if strings.TrimSpace(ctx.IMPU) == "" || ctx.CachedContactHdr == nil {
		t.Fatalf("controller context = %#v", ctx)
	}
	call := NewCall(agent, callstate.DirectionOutbound, "dialog-production", "43430")
	if err := agent.prepareVoiceDialog(call, "43430"); err != nil {
		t.Fatal(err)
	}
	dialog := call.voiceDialogSnapshot()
	if dialog.localURI != ctx.IMPU || dialog.userAgent != strings.TrimSpace(ctx.UserAgent) {
		t.Fatalf("prepared dialog = local %q UA %q; context = local %q UA %q",
			dialog.localURI, dialog.userAgent, ctx.IMPU, ctx.UserAgent)
	}
}

type inboundDialogRegistrar struct {
	conn      *net.UDPConn
	client    chan *net.UDPAddr
	responses chan string
}

func startInboundDialogRegistrar(t *testing.T) *inboundDialogRegistrar {
	t.Helper()
	conn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	registrar := &inboundDialogRegistrar{
		conn: conn, client: make(chan *net.UDPAddr, 1), responses: make(chan string, 8),
	}
	t.Cleanup(func() { _ = conn.Close() })
	go registrar.serve()
	return registrar
}

func (r *inboundDialogRegistrar) serve() {
	buffer := make([]byte, 64*1024)
	for {
		n, remote, err := r.conn.ReadFromUDP(buffer)
		if err != nil {
			return
		}
		message := string(buffer[:n])
		switch {
		case strings.HasPrefix(message, "REGISTER "):
			select {
			case r.client <- remote:
			default:
			}
			r.writeResponse(message, remote, 200, "P-Associated-URI: <sip:user@ims.example.com>\r\n", "")
		case strings.HasPrefix(message, "BYE "):
			r.writeResponse(message, remote, 200, "", "")
		case strings.HasPrefix(message, "SIP/2.0 "):
			r.responses <- message
		}
	}
}

func (r *inboundDialogRegistrar) writeResponse(
	request string,
	remote *net.UDPAddr,
	status int,
	extra string,
	body string,
) {
	response := fmt.Sprintf(
		"SIP/2.0 %d %s\r\nVia: %s\r\nFrom: %s\r\nTo: %s\r\nCall-ID: %s\r\nCSeq: %s\r\n%sContent-Length: %d\r\n\r\n%s",
		status, imscore.SIPStatusText(status), voiceTestHeader(request, "Via"),
		voiceTestHeader(request, "From"), voiceTestHeader(request, "To"),
		voiceTestHeader(request, "Call-ID"), voiceTestHeader(request, "CSeq"),
		extra, len(body), body,
	)
	_, _ = r.conn.WriteToUDP([]byte(response), remote)
}

func (r *inboundDialogRegistrar) waitClient(t *testing.T) *net.UDPAddr {
	t.Helper()
	select {
	case client := <-r.client:
		return client
	case <-time.After(time.Second):
		t.Fatal("registered IMS client address was not observed")
		return nil
	}
}

func (r *inboundDialogRegistrar) waitStatus(t *testing.T, status int) string {
	t.Helper()
	return r.waitResponse(t, func(response string) bool {
		return strings.HasPrefix(response, fmt.Sprintf("SIP/2.0 %d ", status))
	}, fmt.Sprintf("SIP %d", status))
}

func (r *inboundDialogRegistrar) waitResponse(
	t *testing.T,
	match func(string) bool,
	description string,
) string {
	t.Helper()
	deadline := time.After(2 * time.Second)
	for {
		select {
		case response := <-r.responses:
			if match(response) {
				return response
			}
		case <-deadline:
			t.Fatalf("inbound dialog did not receive %s", description)
		}
	}
}

func TestAgentAnswersNetworkInviteThroughServerDialogHandle(t *testing.T) {
	registrar := startInboundDialogRegistrar(t)
	agent := newVoiceTestAgent(t, registrar.conn)
	incoming := make(chan IncomingCall, 1)
	agent.SetIncomingCallHandler(func(call IncomingCall) { incoming <- call })
	if err := agent.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = agent.Stop() })
	clientAddr := registrar.waitClient(t)
	imsPeer := listenVoiceUDP(t)
	clientPeer := listenVoiceUDP(t)
	callID := "dialog-controller-inbound"
	request := inboundNetworkInvite(registrar.conn.LocalAddr().String(), callID, voiceSDP(imsPeer.LocalAddr().(*net.UDPAddr).Port))
	if _, err := registrar.conn.WriteToUDP([]byte(request), clientAddr); err != nil {
		t.Fatal(err)
	}
	provisional := registrar.waitStatus(t, 180)

	var delivered IncomingCall
	select {
	case delivered = <-incoming:
	case <-time.After(2 * time.Second):
		t.Fatal("network INVITE did not reach the Agent")
	}
	if _, err := agent.AnswerWithSDP(delivered.CallID, voiceSDP(clientPeer.LocalAddr().(*net.UDPAddr).Port)); err != nil {
		t.Fatal(err)
	}
	accepted := registrar.waitStatus(t, 200)
	if !strings.Contains(accepted, "Content-Type: application/sdp") || !strings.Contains(accepted, "Contact:") {
		t.Fatalf("inbound 200 is missing dialog headers: %q", accepted)
	}
	if voiceHeaderTag(voiceTestHeader(accepted, "To")) != voiceHeaderTag(voiceTestHeader(provisional, "To")) {
		t.Fatal("provisional and final responses used different local tags")
	}
	ack := inboundNetworkACK(registrar.conn.LocalAddr().String(), callID, voiceTestHeader(accepted, "To"))
	if _, err := registrar.conn.WriteToUDP([]byte(ack), clientAddr); err != nil {
		t.Fatal(err)
	}
	call := agent.callByID(callID)
	if call == nil || call.IMSDialog() == nil {
		t.Fatal("accepted inbound call did not retain its server dialog handle")
	}
}

func TestAgentRejectsNetworkInviteThroughServerTransaction(t *testing.T) {
	registrar := startInboundDialogRegistrar(t)
	agent := newVoiceTestAgent(t, registrar.conn)
	incoming := make(chan IncomingCall, 1)
	agent.SetIncomingCallHandler(func(call IncomingCall) { incoming <- call })
	if err := agent.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = agent.Stop() })
	clientAddr := registrar.waitClient(t)
	imsPeer := listenVoiceUDP(t)
	callID := "dialog-controller-reject"
	request := inboundNetworkInvite(registrar.conn.LocalAddr().String(), callID, voiceSDP(imsPeer.LocalAddr().(*net.UDPAddr).Port))
	if _, err := registrar.conn.WriteToUDP([]byte(request), clientAddr); err != nil {
		t.Fatal(err)
	}
	_ = registrar.waitStatus(t, 180)
	var delivered IncomingCall
	select {
	case delivered = <-incoming:
	case <-time.After(2 * time.Second):
		t.Fatal("network INVITE did not reach the Agent")
	}
	if err := agent.Reject(delivered.CallID, 486); err != nil {
		t.Fatal(err)
	}
	rejected := registrar.waitStatus(t, 486)
	ack := inboundFailureACK(registrar.conn.LocalAddr().String(), callID, voiceTestHeader(rejected, "To"))
	if _, err := registrar.conn.WriteToUDP([]byte(ack), clientAddr); err != nil {
		t.Fatal(err)
	}
	if agent.IsBusy() {
		t.Fatal("rejected network call remained active")
	}
}

func TestAgentHandlesNetworkCancelRaceOnRetainedInvite(t *testing.T) {
	registrar := startInboundDialogRegistrar(t)
	agent := newVoiceTestAgent(t, registrar.conn)
	if err := agent.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = agent.Stop() })
	clientAddr := registrar.waitClient(t)
	imsPeer := listenVoiceUDP(t)
	callID := "dialog-controller-cancel"
	request := inboundNetworkInvite(registrar.conn.LocalAddr().String(), callID, voiceSDP(imsPeer.LocalAddr().(*net.UDPAddr).Port))
	if _, err := registrar.conn.WriteToUDP([]byte(request), clientAddr); err != nil {
		t.Fatal(err)
	}
	provisional := registrar.waitStatus(t, 180)
	cancel := inboundNetworkCancel(registrar.conn.LocalAddr().String(), callID, voiceTestHeader(provisional, "To"))
	if _, err := registrar.conn.WriteToUDP([]byte(cancel), clientAddr); err != nil {
		t.Fatal(err)
	}
	_ = registrar.waitResponse(t, func(response string) bool {
		return strings.HasPrefix(response, "SIP/2.0 200 ") && strings.HasSuffix(voiceTestHeader(response, "CSeq"), " CANCEL")
	}, "CANCEL 200")
	terminated := registrar.waitStatus(t, 487)
	ack := inboundFailureACK(registrar.conn.LocalAddr().String(), callID, voiceTestHeader(terminated, "To"))
	if _, err := registrar.conn.WriteToUDP([]byte(ack), clientAddr); err != nil {
		t.Fatal(err)
	}
	waitForAgentIdle(t, agent)
}

func inboundNetworkInvite(localAddr, callID, sdp string) string {
	return fmt.Sprintf(
		"INVITE sip:user@ims.example.com SIP/2.0\r\nVia: SIP/2.0/UDP %s;rport;branch=z9hG4bK-inbound-28\r\nFrom: <sip:+447700900123@ims.example.com>;tag=remote-28\r\nTo: <sip:user@ims.example.com>\r\nCall-ID: %s\r\nCSeq: 1 INVITE\r\nContact: <sip:peer@%s>\r\nContent-Type: application/sdp\r\nContent-Length: %d\r\n\r\n%s",
		localAddr, callID, localAddr, len(sdp), sdp,
	)
}

func inboundNetworkACK(localAddr, callID, to string) string {
	return fmt.Sprintf(
		"ACK sip:user@ims.example.com SIP/2.0\r\nVia: SIP/2.0/UDP %s;rport;branch=z9hG4bK-ack-28\r\nFrom: <sip:+447700900123@ims.example.com>;tag=remote-28\r\nTo: %s\r\nCall-ID: %s\r\nCSeq: 1 ACK\r\nContent-Length: 0\r\n\r\n",
		localAddr, to, callID,
	)
}

func inboundFailureACK(localAddr, callID, to string) string {
	return fmt.Sprintf(
		"ACK sip:user@ims.example.com SIP/2.0\r\nVia: SIP/2.0/UDP %s;rport;branch=z9hG4bK-inbound-28\r\nFrom: <sip:+447700900123@ims.example.com>;tag=remote-28\r\nTo: %s\r\nCall-ID: %s\r\nCSeq: 1 ACK\r\nContent-Length: 0\r\n\r\n",
		localAddr, to, callID,
	)
}

func inboundNetworkCancel(localAddr, callID, to string) string {
	return fmt.Sprintf(
		"CANCEL sip:user@ims.example.com SIP/2.0\r\nVia: SIP/2.0/UDP %s;rport;branch=z9hG4bK-inbound-28\r\nFrom: <sip:+447700900123@ims.example.com>;tag=remote-28\r\nTo: %s\r\nCall-ID: %s\r\nCSeq: 1 CANCEL\r\nContent-Length: 0\r\n\r\n",
		localAddr, to, callID,
	)
}

func waitForAgentIdle(t *testing.T, agent *Agent) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if !agent.IsBusy() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("network call did not reach a terminal state")
}
