package voice

import (
	"context"
	"fmt"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/emiago/sipgo/sip"
	"github.com/iniwex5/vowifi-go/internal/vowifi/imscore"
	"github.com/iniwex5/vowifi-go/internal/vowifi/voice/callstate"
)

type lateAcceptedRegistrar struct {
	conn       *net.UDPConn
	invite     chan struct{}
	ack        chan string
	bye        chan string
	finalDelay time.Duration
}

func startLateAcceptedRegistrar(t *testing.T) *lateAcceptedRegistrar {
	return startLateAcceptedRegistrarWithDelay(t, 0)
}

func startLateAcceptedRegistrarWithDelay(t *testing.T, finalDelay time.Duration) *lateAcceptedRegistrar {
	t.Helper()
	conn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	registrar := &lateAcceptedRegistrar{
		conn: conn, invite: make(chan struct{}, 1), ack: make(chan string, 1),
		bye: make(chan string, 1), finalDelay: finalDelay,
	}
	t.Cleanup(func() { _ = conn.Close() })
	go registrar.serve()
	return registrar
}

func (r *lateAcceptedRegistrar) serve() {
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
		case "REGISTER":
			r.writeResponse(request, remote, 200, "P-Associated-URI: <sip:user@ims.example.com>\r\n")
		case "INVITE":
			invite, inviteRemote = request, remote
			contact := fmt.Sprintf("<sip:callee@127.0.0.1:%d>", r.conn.LocalAddr().(*net.UDPAddr).Port)
			r.writeResponse(request, remote, 180,
				"To: <sip:callee@ims.example.com>;tag=late-28\r\nContact: "+contact+"\r\n")
			r.invite <- struct{}{}
		case "CANCEL":
			r.writeResponse(request, remote, 200, "")
			go r.writeLateAcceptedInvite(invite, inviteRemote)
		case "ACK":
			r.ack <- request
		case "BYE":
			r.bye <- request
			r.writeResponse(request, remote, 200, "")
		}
	}
}

func (r *lateAcceptedRegistrar) writeLateAcceptedInvite(invite string, remote *net.UDPAddr) {
	if r.finalDelay > 0 {
		timer := time.NewTimer(r.finalDelay)
		defer timer.Stop()
		<-timer.C
	}
	contact := fmt.Sprintf("<sip:callee@127.0.0.1:%d>", r.conn.LocalAddr().(*net.UDPAddr).Port)
	r.writeResponse(invite, remote, 200,
		"To: <sip:callee@ims.example.com>;tag=late-28\r\nContact: "+contact+"\r\n")
}

func (r *lateAcceptedRegistrar) writeResponse(request string, remote *net.UDPAddr, status int, extra string) {
	response := fmt.Sprintf(
		"SIP/2.0 %d %s\r\nVia: %s\r\nCall-ID: %s\r\nCSeq: %s\r\n%sContent-Length: 0\r\n\r\n",
		status, imscore.SIPStatusText(status), voiceTestHeader(request, "Via"),
		voiceTestHeader(request, "Call-ID"), voiceTestHeader(request, "CSeq"), extra,
	)
	_, _ = r.conn.WriteToUDP([]byte(response), remote)
}

func TestAgentACKsAndBYEsInviteAcceptedAfterCancel(t *testing.T) {
	registrar := startLateAcceptedRegistrar(t)
	agent := newVoiceTestAgent(t, registrar.conn)
	if err := agent.StartCurrent(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = agent.Stop() })
	dialResult := make(chan error, 1)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_, err := agent.dialContext(ctx, "43430", testClientSDP)
		dialResult <- err
	}()
	select {
	case <-registrar.invite:
	case <-time.After(time.Second):
		t.Fatal("outbound INVITE was not observed")
	}
	call := waitForCancelableCall(t, agent)
	if err := agent.HandleClientCancel(call.CallID()); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-dialResult:
		if err == nil || !strings.Contains(err.Error(), "accepted after local CANCEL") {
			t.Fatalf("dial result after late 2xx = %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("dial did not finish after late 2xx")
	}
	select {
	case <-registrar.ack:
	case <-time.After(time.Second):
		t.Fatal("late 2xx was not ACKed")
	}
	select {
	case <-registrar.bye:
	case <-time.After(time.Second):
		t.Fatal("late accepted dialog was not closed with BYE")
	}
	if !call.HasLocalCancelSent() || !call.IsACKSent() || agent.IsBusy() || call.IMSDialog() != nil {
		t.Fatalf("late 2xx cleanup: cancel=%t ack=%t busy=%t dialog=%v",
			call.HasLocalCancelSent(), call.IsACKSent(), agent.IsBusy(), call.IMSDialog())
	}
}

func TestAgentHangupPendingInviteUsesCancelTerminalState(t *testing.T) {
	registrar := startLateAcceptedRegistrar(t)
	agent := newVoiceTestAgent(t, registrar.conn)
	if err := agent.StartCurrent(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = agent.Stop() })
	dialResult := make(chan error, 1)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_, err := agent.dialContext(ctx, "43430", testClientSDP)
		dialResult <- err
	}()
	select {
	case <-registrar.invite:
	case <-time.After(time.Second):
		t.Fatal("outbound INVITE was not observed")
	}
	call := waitForCancelableCall(t, agent)
	if err := agent.HangupCurrent(call.CallID()); err != nil {
		t.Fatalf("Hangup pending INVITE: %v", err)
	}
	if call.CallState() != callstate.StateTerminated || agent.IsBusy() {
		t.Fatalf("pending hangup state=%s busy=%t", call.CallState(), agent.IsBusy())
	}
	select {
	case <-call.Done:
	default:
		t.Fatal("pending hangup left call completion open")
	}
	select {
	case err := <-dialResult:
		if err == nil || !strings.Contains(err.Error(), "accepted after local CANCEL") {
			t.Fatalf("dial result after pending hangup = %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("dial did not finish after pending hangup")
	}
}

func TestCancelSettleStillACKsAndBYEsLaterAcceptedInvite(t *testing.T) {
	registrar := startLateAcceptedRegistrarWithDelay(t, 80*time.Millisecond)
	agent := newVoiceTestAgent(t, registrar.conn)
	agent.outboundCancelSettle = 20 * time.Millisecond
	if err := agent.StartCurrent(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = agent.Stop() })

	inviteTx := newVoiceServerTransaction()
	request := mustClientRequest(t, sip.INVITE, "cancel-settle-late-2xx", testClientSDP, "")
	agent.HandleOutboundInvite(request, inviteTx)
	waitVoiceResponse(t, inviteTx, 180)
	call := waitForCancelableCall(t, agent)
	cancelTx := newVoiceServerTransaction()
	agent.HandleCancel(mustClientRequest(t, sip.CANCEL, call.ClientCallID(), "", ""), cancelTx)
	waitVoiceResponse(t, cancelTx, 200)
	waitVoiceResponse(t, inviteTx, 487)

	select {
	case <-registrar.ack:
	case <-time.After(time.Second):
		t.Fatal("accepted INVITE after settle was not ACKed")
	}
	select {
	case <-registrar.bye:
	case <-time.After(time.Second):
		t.Fatal("accepted INVITE after settle was not closed with BYE")
	}
	waitForLateAcceptedDialogClose(t, call)
	if !callDone(call) || agent.IsBusy() || call.IMSDialog() != nil {
		t.Fatalf("settled cancel cleanup: done=%t busy=%t dialog=%v",
			callDone(call), agent.IsBusy(), call.IMSDialog())
	}
	select {
	case response := <-inviteTx.responses:
		t.Fatalf("local INVITE received duplicate final %d", response.StatusCode)
	case <-time.After(20 * time.Millisecond):
	}
}

func waitForLateAcceptedDialogClose(t *testing.T, call *Call) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if call.IMSDialog() == nil {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("late accepted dialog was not released after BYE completed")
}

func waitForCancelableCall(t *testing.T, agent *Agent) *Call {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		call := agent.ActiveCall()
		if call != nil && call.IMSInviteHandle() != nil {
			return call
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("outbound call did not retain an INVITE handle")
	return nil
}
