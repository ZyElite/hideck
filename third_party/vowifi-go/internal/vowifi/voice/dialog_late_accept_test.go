package voice

import (
	"context"
	"fmt"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/iniwex5/vowifi-go/internal/vowifi/imscore"
)

type lateAcceptedRegistrar struct {
	conn   *net.UDPConn
	invite chan struct{}
	ack    chan string
	bye    chan string
}

func startLateAcceptedRegistrar(t *testing.T) *lateAcceptedRegistrar {
	t.Helper()
	conn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	registrar := &lateAcceptedRegistrar{
		conn: conn, invite: make(chan struct{}, 1), ack: make(chan string, 1), bye: make(chan string, 1),
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
			contact := fmt.Sprintf("<sip:callee@127.0.0.1:%d>", r.conn.LocalAddr().(*net.UDPAddr).Port)
			r.writeResponse(invite, inviteRemote, 200,
				"To: <sip:callee@ims.example.com>;tag=late-28\r\nContact: "+contact+"\r\n")
		case "ACK":
			r.ack <- request
		case "BYE":
			r.bye <- request
			r.writeResponse(request, remote, 200, "")
		}
	}
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
	if err := agent.Start(); err != nil {
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
