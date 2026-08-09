package sipkit

import (
	"errors"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/emiago/sipgo/sip"
)

func TestBuildMinimalDialogRequestFiltersOwnedHeaders(t *testing.T) {
	request, err := BuildMinimalDialogRequest(sip.BYE, mustURI(t, "sip:peer@example.com"), DialogRequestOptions{
		PAccessNetworkInfo: "3GPP-NR", PreferredIdentity: "<sip:user@example.com>",
		SecurityVerify: "ipsec-3gpp;spi-c=1", Protected: true, UserAgent: "VoHive",
		Headers: []sip.Header{sip.NewHeader("Via", "SIP/2.0/UDP old"), sip.NewHeader("Session-ID", "abc")},
	})
	if err != nil {
		t.Fatalf("BuildMinimalDialogRequest: %v", err)
	}
	if request.Via() != nil || FirstHeaderValue(request, "Session-ID", true) != "abc" {
		t.Fatal("dialog-owned filtering mismatch")
	}
	for _, name := range []string{"P-Access-Network-Info", "P-Preferred-Identity", "Require", "Proxy-Require", "Security-Verify"} {
		if FirstHeaderValue(request, name, true) == "" {
			t.Errorf("missing %s", name)
		}
	}
}

func TestResolveViaRouteUsesReceivedAndRPort(t *testing.T) {
	params := sip.NewParams()
	params.Add("received", "2001:db8::2")
	params.Add("rport", "5090")
	transport, destination, source, err := ResolveViaRoute(&sip.ViaHeader{
		Transport: "tcp", Host: "sent.example", Port: 5060, Params: params,
	})
	if err != nil || transport != "TCP" || destination != "[2001:db8::2]:5090" || source != "received" {
		t.Fatalf("ResolveViaRoute = %q %q %q %v", transport, destination, source, err)
	}
}

func TestDispatchResponseTransactionAndFallback(t *testing.T) {
	response := responseWithVia("127.0.0.1", 5060)
	tx := &serverTransactionStub{}
	mode, _, _, err := DispatchResponseByVia(response, tx, func(*sip.Response) error {
		t.Fatal("stateless writer called after transaction success")
		return nil
	})
	if err != nil || mode != responseModeTransaction || tx.responses != 1 {
		t.Fatalf("transaction dispatch = %q responses=%d err=%v", mode, tx.responses, err)
	}
	tx.respondErr = errors.New("closed")
	written := false
	mode, transport, destination, err := DispatchResponseByVia(response, tx, func(got *sip.Response) error {
		written = got == response
		return nil
	})
	if err != nil || mode != responseModeStatelessVia || transport != "UDP" || destination != "127.0.0.1:5060" || !written {
		t.Fatalf("fallback dispatch = %q %q %q %v written=%v", mode, transport, destination, err, written)
	}
}

func TestWriteResponseByViaUDP(t *testing.T) {
	listener, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	port := listener.LocalAddr().(*net.UDPAddr).Port
	response := responseWithVia("127.0.0.1", port)
	transport, destination, source, err := WriteResponseByVia(response, time.Second)
	if err != nil {
		t.Fatalf("WriteResponseByVia: %v", err)
	}
	buffer := make([]byte, 2048)
	_ = listener.SetReadDeadline(time.Now().Add(time.Second))
	count, _, err := listener.ReadFrom(buffer)
	if err != nil {
		t.Fatal(err)
	}
	if transport != "UDP" || source != "sent-by" || !strings.Contains(destination, ":") || !strings.HasPrefix(string(buffer[:count]), "SIP/2.0 200 OK") {
		t.Fatalf("write route=%q %q %q packet=%q", transport, destination, source, buffer[:count])
	}
}

func responseWithVia(host string, port int) *sip.Response {
	response := sip.NewResponse(200, "OK")
	response.AppendHeader(&sip.ViaHeader{
		ProtocolName: "SIP", ProtocolVersion: "2.0", Transport: "UDP", Host: host, Port: port,
	})
	response.SetBody(nil)
	return response
}

type serverTransactionStub struct {
	respondErr error
	responses  int
}

func (stub *serverTransactionStub) Terminate()                         {}
func (stub *serverTransactionStub) OnTerminate(sip.FnTxTerminate) bool { return true }
func (stub *serverTransactionStub) Done() <-chan struct{}              { return make(chan struct{}) }
func (stub *serverTransactionStub) Err() error                         { return nil }
func (stub *serverTransactionStub) Acks() <-chan *sip.Request          { return make(chan *sip.Request) }
func (stub *serverTransactionStub) OnCancel(sip.FnTxCancel) bool       { return true }
func (stub *serverTransactionStub) Respond(*sip.Response) error {
	stub.responses++
	return stub.respondErr
}
