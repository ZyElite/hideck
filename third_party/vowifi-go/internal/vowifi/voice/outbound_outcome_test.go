package voice

import (
	"context"
	"fmt"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/emiago/sipgo/sip"
	"github.com/iniwex5/vowifi-go/internal/vowifi/imscore"
)

func TestOutboundInviteOutcomeClassification(t *testing.T) {
	tests := []struct {
		name         string
		status       int
		reason       string
		cancelReason string
		noAnswer     bool
		wantKind     string
		wantText     string
	}{
		{name: "temporarily unavailable", status: 480, reason: "Temporarily Unavailable", wantKind: "temporarily_unavailable", wantText: "暂时无法接通 (480 Temporarily Unavailable)"},
		{name: "busy", status: 486, reason: "Busy Here", wantKind: "busy", wantText: "对方忙线 (486 Busy Here)"},
		{name: "global busy", status: 600, reason: "Busy Everywhere", wantKind: "busy", wantText: "对方忙线 (600 Busy Everywhere)"},
		{name: "declined", status: 603, reason: "Decline", wantKind: "declined", wantText: "对方拒接 (603 Decline)"},
		{name: "redirected", status: 302, reason: "Moved Temporarily", wantKind: "redirected", wantText: "呼叫失败 (302 Moved Temporarily)"},
		{name: "no answer flag", status: 487, reason: "Request Terminated", noAnswer: true, wantKind: "no_answer", wantText: "无人接听（30s 超时）"},
		{name: "no answer marker", cancelReason: "no_answer", wantKind: "no_answer", wantText: "无人接听（30s 超时）"},
		{name: "failed", status: 500, reason: "Server Internal Error", wantKind: "failed", wantText: "呼叫失败 (500 Server Internal Error)"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			kind, status, reason, cancelReason := classifyOutboundInviteOutcome(
				test.status, test.reason, test.cancelReason, test.noAnswer,
			)
			if kind != test.wantKind {
				t.Fatalf("kind = %q, want %q", kind, test.wantKind)
			}
			if got := formatSimulateCallReason(kind, status, reason, cancelReason); got != test.wantText {
				t.Fatalf("reason = %q, want %q", got, test.wantText)
			}
		})
	}
}

func TestStructuredRejectedInviteRecordsTransactionACK(t *testing.T) {
	registrar := startControlledRejectingRegistrar(t, 486)
	agent := newVoiceTestAgent(t, registrar.conn)
	if err := agent.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = agent.Stop() })
	transaction := newVoiceServerTransaction()
	request := mustClientRequest(t, sip.INVITE, "rejected-outbound-31", testClientSDP, "")
	agent.HandleOutboundInvite(request, transaction)

	select {
	case <-registrar.invite:
	case <-time.After(time.Second):
		t.Fatal("outbound INVITE was not observed")
	}
	call := agent.ActiveCall()
	if call == nil {
		t.Fatal("outbound call was not retained during INVITE transaction")
	}
	registrar.releaseResponse()
	waitVoiceResponse(t, transaction, 486)
	select {
	case <-registrar.ack:
	case <-time.After(time.Second):
		t.Fatal("non-2xx INVITE response was not ACKed")
	}
	call.mu.RLock()
	errorACKSent := call.DialogState.ErrorACKSent
	call.mu.RUnlock()
	if !errorACKSent || !call.IsACKSent() || agent.IsBusy() {
		t.Fatalf("rejection cleanup: error_ack=%t ack=%t busy=%t",
			errorACKSent, call.IsACKSent(), agent.IsBusy())
	}
}

func TestSimulatedCallReportsEarlyTermination(t *testing.T) {
	call := NewCall(nil, 1, "simulated-early-end", "43430")
	call.CloseDone()
	result, err := (&Agent{}).holdSimulatedCall(
		context.Background(), call, 30, time.Now(),
	)
	if err != nil {
		t.Fatal(err)
	}
	if result == nil || result.Success || result.Reason != "中途被动终止" {
		t.Fatalf("simulate result = %+v", result)
	}
}

type controlledRejectingRegistrar struct {
	conn        *net.UDPConn
	status      int
	invite      chan struct{}
	ack         chan struct{}
	release     chan struct{}
	releaseOnce sync.Once
}

func startControlledRejectingRegistrar(t *testing.T, status int) *controlledRejectingRegistrar {
	t.Helper()
	conn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	registrar := &controlledRejectingRegistrar{
		conn: conn, status: status, invite: make(chan struct{}, 1),
		ack: make(chan struct{}, 1), release: make(chan struct{}),
	}
	t.Cleanup(func() {
		registrar.releaseResponse()
		_ = conn.Close()
	})
	go registrar.serve()
	return registrar
}

func (r *controlledRejectingRegistrar) releaseResponse() {
	r.releaseOnce.Do(func() { close(r.release) })
}

func (r *controlledRejectingRegistrar) serve() {
	buffer := make([]byte, 64*1024)
	for {
		n, remote, err := r.conn.ReadFromUDP(buffer)
		if err != nil {
			return
		}
		request := string(buffer[:n])
		switch sipMethodForTest(request) {
		case "REGISTER":
			r.writeResponse(request, remote, 200,
				"P-Associated-URI: <sip:user@ims.example.com>\r\n")
		case "INVITE":
			r.invite <- struct{}{}
			<-r.release
			r.writeResponse(request, remote, r.status,
				"To: <sip:callee@ims.example.com>;tag=rejected-31\r\n")
		case "ACK":
			select {
			case r.ack <- struct{}{}:
			default:
			}
		}
	}
}

func (r *controlledRejectingRegistrar) writeResponse(
	request string,
	remote *net.UDPAddr,
	status int,
	extra string,
) {
	reason := imscore.SIPStatusText(status)
	response := fmt.Sprintf(
		"SIP/2.0 %d %s\r\nVia: %s\r\nCall-ID: %s\r\nCSeq: %s\r\n%sContent-Length: 0\r\n\r\n",
		status, reason, voiceTestHeader(request, "Via"), voiceTestHeader(request, "Call-ID"),
		voiceTestHeader(request, "CSeq"), strings.TrimSpace(extra)+"\r\n",
	)
	_, _ = r.conn.WriteToUDP([]byte(response), remote)
}
