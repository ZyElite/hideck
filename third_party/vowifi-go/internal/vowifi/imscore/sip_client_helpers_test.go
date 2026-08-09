package imscore

import (
	"strings"
	"testing"

	"github.com/emiago/sipgo"
	"github.com/emiago/sipgo/sip"
)

func TestClientBuildRequestRejectsNilAndSkipsRegister(t *testing.T) {
	if err := clientBuildRequest(nil, nil); err == nil || err.Error() != "client/request 为空" {
		t.Fatalf("nil error = %v", err)
	}
	client := newSIPGoBuildClient(t)
	request := sip.NewRequest(sip.REGISTER, sip.Uri{Scheme: "sip", Host: "ims.example"})
	if err := clientBuildRequest(client, request); err != nil {
		t.Fatal(err)
	}
	if request.Via() != nil || request.CallID() != nil || request.CSeq() != nil {
		t.Fatalf("REGISTER was modified: %s", request.String())
	}
}

func TestClientBuildRequestCompletesNonRegisterTransactionHeaders(t *testing.T) {
	client := newSIPGoBuildClient(t)
	request := sip.NewRequest(sip.OPTIONS, sip.Uri{Scheme: "sip", User: "peer", Host: "ims.example"})
	if err := clientBuildRequest(client, request); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"Via", "From", "To", "Call-ID", "CSeq", "Max-Forwards", "Content-Length"} {
		if request.GetHeader(name) == nil {
			t.Fatalf("built request has no %s: %s", name, request.String())
		}
	}
	if !strings.HasSuffix(request.CSeq().Value(), " OPTIONS") {
		t.Fatalf("CSeq = %q", request.CSeq().Value())
	}
}

func newSIPGoBuildClient(t *testing.T) *sipgo.Client {
	t.Helper()
	ua, err := sipgo.NewUA(sipgo.WithUserAgent("transaction-test"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ua.Close() })
	client, err := sipgo.NewClient(ua,
		sipgo.WithClientHostname("127.0.0.1"), sipgo.WithClientPort(5060))
	if err != nil {
		t.Fatal(err)
	}
	return client
}
