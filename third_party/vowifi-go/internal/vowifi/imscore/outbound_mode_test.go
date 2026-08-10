package imscore

import (
	"context"
	"errors"
	"io"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/emiago/sipgo/sip"
)

func TestOutboundModeResolveErrorPreservesCodeAndCause(t *testing.T) {
	cause := errors.New("transport unavailable")
	err := newOutboundModeResolveError("missing_transport", "resolve mode: %w", cause)
	if got := outboundResolveErrorCode(err); got != "missing_transport" {
		t.Fatalf("resolve code = %q", got)
	}
	if !errors.Is(err, cause) || err.Error() != "resolve mode: transport unavailable" {
		t.Fatalf("resolve error = %v", err)
	}
	code, message := modeContextErrorFields(err)
	if code != "missing_transport" || message != err.Error() {
		t.Fatalf("error fields = %q, %q", code, message)
	}
}

func TestTransportForRequestMatchesRecoveredSelection(t *testing.T) {
	tests := []struct {
		configured string
		security   string
		method     sip.RequestMethod
		want       string
	}{
		{configured: "auto", security: securityModePlain, method: sip.MESSAGE, want: "UDP"},
		{configured: "auto", security: securityModeIPSec, method: sip.MESSAGE, want: "TCP"},
		{configured: "udp", security: securityModeIPSec, method: sip.OPTIONS, want: "TCP"},
		{configured: "udp", security: securityModeIPSec, method: sip.SUBSCRIBE, want: "TCP"},
		{configured: "udp", security: securityModeIPSec, method: sip.MESSAGE, want: "UDP"},
		{configured: "tcp", security: securityModePlain, method: sip.MESSAGE, want: "TCP"},
	}
	for _, test := range tests {
		if got := transportForRequest(test.configured, test.security, test.method); got != test.want {
			t.Errorf("transportForRequest(%q, %q, %s) = %q, want %q",
				test.configured, test.security, test.method, got, test.want)
		}
	}
}

func TestSendDirectTCPSafeUsesContextDeadlineAndClearsIt(t *testing.T) {
	client, peer := net.Pipe()
	t.Cleanup(func() { _ = client.Close() })
	t.Cleanup(func() { _ = peer.Close() })
	tracked := &writeDeadlineConn{Conn: client}
	payload := []byte("OPTIONS sip:ims.example SIP/2.0\r\n\r\n")
	readDone := make(chan error, 1)
	go func() {
		received := make([]byte, len(payload))
		_, err := io.ReadFull(peer, received)
		if err == nil && string(received) != string(payload) {
			err = errors.New("direct TCP payload mismatch")
		}
		readDone <- err
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	if err := sendDirectTCPSafe(directTCPWriteOptions{
		Context: ctx, Conn: tracked, Payload: payload, Timeout: outboundDirectWriteTimeout,
	}); err != nil {
		t.Fatal(err)
	}
	if err := <-readDone; err != nil {
		t.Fatal(err)
	}
	deadlines := tracked.snapshotDeadlines()
	if len(deadlines) != 2 || deadlines[0].IsZero() || !deadlines[1].IsZero() {
		t.Fatalf("write deadlines = %v", deadlines)
	}
	contextDeadline, _ := ctx.Deadline()
	if deadlines[0].After(contextDeadline) {
		t.Fatalf("write deadline %s exceeds context deadline %s", deadlines[0], contextDeadline)
	}
}

func TestSendDirectWriteExposesMissingChannelAndCanceledContext(t *testing.T) {
	err := sendDirectWrite(context.Background(), outboundModeContext{}, "MESSAGE sip:x SIP/2.0\r\n\r\n")
	if err == nil || err.Error() != "no direct write channel available" {
		t.Fatalf("missing channel error = %v", err)
	}
	client, peer := net.Pipe()
	defer client.Close()
	defer peer.Close()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err = sendDirectWrite(ctx, outboundModeContext{TCPConn: client}, "MESSAGE sip:x SIP/2.0\r\n\r\n")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled direct write error = %v", err)
	}
}

func TestWriteStatelessWithSipgoUsesResolvedExternalSender(t *testing.T) {
	service, err := New(&IMSConfig{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(service.Stop)
	request := parsedDispatchRequest(t, "stateless-message", 1)
	originalDestination := request.Destination()
	var sent string
	modeCtx := outboundModeContext{
		Mode: "external", SignalingReady: true, RemoteIP: "192.0.2.44", RemotePortS: 5060,
		send: func(_ context.Context, raw string) error {
			sent = raw
			return nil
		},
	}
	if err := service.writeStatelessWithSipgo(context.Background(), modeCtx, request); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(sent, "MESSAGE ") || rawSIPHeaderValue(sent, "Call-ID") != "stateless-message" {
		t.Fatalf("stateless request = %q", sent)
	}
	if request.Destination() != originalDestination {
		t.Fatalf("source request destination changed from %q to %q", originalDestination, request.Destination())
	}
}

func TestSetOutboundDestinationUsesSnapshotWithoutMutatingEmptyDestination(t *testing.T) {
	request := parsedDispatchRequest(t, "destination-snapshot", 1)
	setOutboundDestination(request, outboundModeContext{RemoteIP: "2001:db8::44", RemotePortS: 5070})
	if got := request.Destination(); got != "[2001:db8::44]:5070" {
		t.Fatalf("destination = %q", got)
	}
	setOutboundDestination(request, outboundModeContext{})
	if got := request.Destination(); got != "[2001:db8::44]:5070" {
		t.Fatalf("empty snapshot changed destination to %q", got)
	}
}

type writeDeadlineConn struct {
	net.Conn
	mu        sync.Mutex
	deadlines []time.Time
}

func (conn *writeDeadlineConn) SetWriteDeadline(deadline time.Time) error {
	conn.mu.Lock()
	conn.deadlines = append(conn.deadlines, deadline)
	conn.mu.Unlock()
	return conn.Conn.SetWriteDeadline(deadline)
}

func (conn *writeDeadlineConn) snapshotDeadlines() []time.Time {
	conn.mu.Lock()
	defer conn.mu.Unlock()
	return append([]time.Time(nil), conn.deadlines...)
}
