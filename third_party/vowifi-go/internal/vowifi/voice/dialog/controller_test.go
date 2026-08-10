package dialog

import (
	"context"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/emiago/sipgo/sip"
	"github.com/iniwex5/vowifi-go/internal/vowifi/imsendpoint"
)

type fakeInviteHandle string

func (h fakeInviteHandle) InviteID() string { return string(h) }

type fakeDialogHandle string

func (h fakeDialogHandle) DialogID() string { return string(h) }

type endpointCall struct {
	method   string
	deviceID string
	value    any
}

type fakeEndpoint struct {
	mu       sync.Mutex
	snapshot imsendpoint.Snapshot
	cseq     uint32
	calls    []endpointCall
	response *sip.Response
	dialog   imsendpoint.DialogHandle
}

func (e *fakeEndpoint) record(method, deviceID string, value any) {
	e.mu.Lock()
	e.calls = append(e.calls, endpointCall{method: method, deviceID: deviceID, value: value})
	e.mu.Unlock()
}

func (e *fakeEndpoint) callsSnapshot() []endpointCall {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]endpointCall(nil), e.calls...)
}

func (e *fakeEndpoint) setSnapshot(snapshot imsendpoint.Snapshot) {
	e.mu.Lock()
	e.snapshot = snapshot
	e.mu.Unlock()
}

func (e *fakeEndpoint) Snapshot() imsendpoint.Snapshot {
	e.mu.Lock()
	defer e.mu.Unlock()
	snapshot := e.snapshot
	snapshot.Voice.ContactParamOrder = append([]string(nil), snapshot.Voice.ContactParamOrder...)
	return snapshot
}

func (e *fakeEndpoint) DeviceID() string   { return "endpoint-device" }
func (e *fakeEndpoint) IsRegistered() bool { return true }

func (e *fakeEndpoint) NextCSeq() uint32 {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.cseq++
	return e.cseq
}

func (e *fakeEndpoint) SendDialogRequest(
	_ context.Context,
	deviceID string,
	_ imsendpoint.DialogHandle,
	request *sip.Request,
	options imsendpoint.DialogRequestOptions,
) (*sip.Response, error) {
	e.record("request", deviceID, struct {
		request *sip.Request
		options imsendpoint.DialogRequestOptions
	}{request: request, options: options})
	return e.response, nil
}

func (e *fakeEndpoint) SendReliableProvisionalPRACK(
	_ context.Context,
	deviceID string,
	options imsendpoint.ReliableProvisionalOptions,
) error {
	e.record("prack", deviceID, options)
	return nil
}

func (e *fakeEndpoint) CloseDialog(
	_ context.Context,
	deviceID string,
	dialog imsendpoint.DialogHandle,
) error {
	e.record("close", deviceID, dialog)
	return nil
}

func (e *fakeEndpoint) CancelClientInvite(
	_ context.Context,
	deviceID string,
	invite imsendpoint.InviteHandle,
	options imsendpoint.ClientInviteCancelOptions,
) error {
	e.record("cancel", deviceID, struct {
		invite  imsendpoint.InviteHandle
		options imsendpoint.ClientInviteCancelOptions
	}{invite: invite, options: options})
	return nil
}

func (e *fakeEndpoint) AnswerServerInvite(
	_ context.Context,
	deviceID string,
	_ imsendpoint.ServerInviteHandle,
	options imsendpoint.ServerInviteAnswerOptions,
) (imsendpoint.DialogHandle, error) {
	e.record("answer", deviceID, options)
	return e.dialog, nil
}

func (e *fakeEndpoint) RejectServerInvite(
	_ context.Context,
	deviceID string,
	_ imsendpoint.ServerInviteHandle,
	options imsendpoint.ServerInviteRejectOptions,
) error {
	e.record("reject", deviceID, options)
	return nil
}

func (e *fakeEndpoint) StartClientInvite(
	context.Context,
	string,
	imsendpoint.ClientInviteOptions,
) (*imsendpoint.ClientInviteResult, error) {
	return nil, nil
}

func TestControllerContextBuildsAndInvalidatesRegistrationHeaders(t *testing.T) {
	endpoint := &fakeEndpoint{}
	endpoint.setSnapshot(testSnapshot("sip:alice@ims.example", "<sip:edge-a.example;lr>"))
	controller := NewController(" dev-28 ", endpoint)

	ctx := controller.Context()
	if ctx.DeviceID != "dev-28" || ctx.CachedFromURI.String() != "sip:alice@ims.example" {
		t.Fatalf("identity context = device %q URI %q", ctx.DeviceID, ctx.CachedFromURI.String())
	}
	contact := ctx.CachedContactHdr
	if contact == nil || strings.Trim(contact.Address.Host, "[]") != "2001:db8::28" || contact.Address.Port != 5064 {
		t.Fatalf("Contact = %#v", contact)
	}
	if transport, _ := contact.Address.UriParams.Get("transport"); transport != "tcp" {
		t.Fatalf("Contact transport = %q", transport)
	}
	wantParams := []string{"+g.3gpp.accesstype", "+sip.instance", "audio", "+g.3gpp.smsip", "+g.3gpp.icsi-ref"}
	if got := contact.Params.Keys(); !reflect.DeepEqual(got, wantParams) {
		t.Fatalf("Contact params = %v, want %v", got, wantParams)
	}
	assertHeader(t, ctx.CachedRouteHdr, "Route", "<sip:edge-a.example;lr>")
	assertHeader(t, ctx.CachedSecVerifyHdr, "Security-Verify", "ipsec-3gpp;alg=hmac-sha-1-96")
	assertHeader(t, ctx.CachedPANIHdr, "P-Access-Network-Info", "IEEE-802.11")
	assertHeader(t, ctx.CachedPPIHdr, "P-Preferred-Identity", "<sip:alice@ims.example>")

	firstContact := ctx.CachedContactHdr
	endpoint.setSnapshot(testSnapshot("sip:bob@ims.example", "<sip:edge-b.example;lr>"))
	refreshed := controller.Context()
	if refreshed.CachedContactHdr == firstContact {
		t.Fatal("registration change retained the stale Contact cache")
	}
	if refreshed.CachedFromURI.String() != "sip:bob@ims.example" {
		t.Fatalf("refreshed From URI = %q", refreshed.CachedFromURI.String())
	}
	assertHeader(t, refreshed.CachedRouteHdr, "Route", "<sip:edge-b.example;lr>")
}

func TestControllerDelegatesDialogLifecycleWithRecoveredDeviceDefaults(t *testing.T) {
	response := sip.NewResponse(200, "OK")
	dialog := fakeDialogHandle("dialog-28")
	invite := fakeInviteHandle("invite-28")
	endpoint := &fakeEndpoint{response: response, dialog: dialog}
	endpoint.setSnapshot(imsendpoint.Snapshot{UserAgent: " Vowifi/1.5.5 "})
	controller := NewController("controller-device", endpoint)
	request := sip.NewRequest(sip.BYE, sip.Uri{Scheme: "sip", Host: "ims.example"})

	got, err := controller.SendDialogRequestWithHandle(
		t.Context(), " ", dialog, request, imsendpoint.DialogRequestOptions{Timeout: 28},
	)
	if err != nil || got != response {
		t.Fatalf("SendDialogRequestWithHandle = (%v, %v)", got, err)
	}
	if err := controller.SendReliableProvisionalPRACK(t.Context(), "", imsendpoint.ReliableProvisionalOptions{Invite: invite, Dialog: dialog}); err != nil {
		t.Fatal(err)
	}
	if err := controller.CloseDialog(t.Context(), "", dialog); err != nil {
		t.Fatal(err)
	}
	if err := controller.CancelClientInvite(t.Context(), "", invite, imsendpoint.ClientInviteCancelOptions{Reason: "user"}); err != nil {
		t.Fatal(err)
	}
	answered, err := controller.AnswerServerInvite(t.Context(), "", invite, imsendpoint.ServerInviteAnswerOptions{Response: response})
	if err != nil || answered != dialog {
		t.Fatalf("AnswerServerInvite = (%v, %v)", answered, err)
	}
	if err := controller.RejectServerInvite(t.Context(), "", invite, imsendpoint.ServerInviteRejectOptions{Code: 486}); err != nil {
		t.Fatal(err)
	}

	want := []string{dialogRequestDevice, prackDevice, closeDialogDevice, cancelInviteDevice, answerInviteDevice, rejectInviteDevice}
	calls := endpoint.callsSnapshot()
	if len(calls) != len(want) {
		t.Fatalf("endpoint call count = %d, want %d", len(calls), len(want))
	}
	for index, deviceID := range want {
		if calls[index].deviceID != deviceID {
			t.Fatalf("call %d device = %q, want %q", index, calls[index].deviceID, deviceID)
		}
	}
	if controller.NextCSeq() != 1 || controller.UserAgent() != "Vowifi/1.5.5" {
		t.Fatalf("endpoint accessors = cseq %d UA %q", endpoint.cseq, controller.UserAgent())
	}
}

func TestControllerExposesMissingContextErrors(t *testing.T) {
	controller := NewController("dev", nil)
	request := sip.NewRequest(sip.ACK, sip.Uri{Scheme: "sip", Host: "ims.example"})
	if _, err := controller.SendDialogRequestWithHandle(t.Context(), "dev", nil, nil, imsendpoint.DialogRequestOptions{}); err == nil || err.Error() != "SIP request 为空" {
		t.Fatalf("nil request error = %v", err)
	}
	if _, err := controller.SendDialogRequestWithHandle(t.Context(), "dev", nil, request, imsendpoint.DialogRequestOptions{}); err == nil {
		t.Fatal("missing endpoint request unexpectedly succeeded")
	}
	if err := controller.SendReliableProvisionalPRACK(t.Context(), "dev", imsendpoint.ReliableProvisionalOptions{}); err == nil {
		t.Fatal("missing endpoint PRACK unexpectedly succeeded")
	}
}

func TestControllerEndpointReplacementIsConcurrentSafe(t *testing.T) {
	first := &fakeEndpoint{}
	first.setSnapshot(testSnapshot("sip:first@ims.example", "<sip:first.example;lr>"))
	second := &fakeEndpoint{}
	second.setSnapshot(testSnapshot("sip:second@ims.example", "<sip:second.example;lr>"))
	controller := NewController("dev", first)
	var wait sync.WaitGroup
	for index := 0; index < 20; index++ {
		wait.Add(2)
		go func() {
			defer wait.Done()
			controller.SetEndpoint(second)
		}()
		go func() {
			defer wait.Done()
			_ = controller.Context()
		}()
	}
	wait.Wait()
	if got := controller.Context().IMPU; got != "sip:second@ims.example" {
		t.Fatalf("final endpoint IMPU = %q", got)
	}
}

func TestEndpointLocalIP(t *testing.T) {
	tests := map[string]string{
		"192.0.2.28:5062":     "192.0.2.28",
		"192.0.2.28":          "192.0.2.28",
		"ims.example:5062":    "ims.example",
		"[2001:db8::28]:5062": "2001:db8::28",
		"[2001:db8::28]":      "2001:db8::28",
		"2001:db8::28":        "2001:db8::28",
	}
	for input, want := range tests {
		t.Run(input, func(t *testing.T) {
			if got := endpointLocalIP(input); got != want {
				t.Fatalf("endpointLocalIP(%q) = %q, want %q", input, got, want)
			}
		})
	}
}

func testSnapshot(impu, route string) imsendpoint.Snapshot {
	return imsendpoint.Snapshot{
		IMPU: impu, Realm: "ims.example", ContactID: "alice", ServiceRoute: route,
		SecVerify: "ipsec-3gpp;alg=hmac-sha-1-96", EffectiveSecMode: "ipsec-3gpp",
		PAccessNetworkInfo: "IEEE-802.11", UserAgent: " Vowifi/1.5.5 ",
		LocalAddr: "[2001:db8::28]:5062", LocalPortC: 5062, LocalPortS: 5064,
		Transport: "TCP", IMEI: "490154203237518",
		Voice: imsendpoint.VoiceProfile{AccessType: "wifi"},
	}
}

func assertHeader(t *testing.T, header sip.Header, name, value string) {
	t.Helper()
	if header == nil || header.Name() != name || header.Value() != value {
		t.Fatalf("%s header = %#v", name, header)
	}
}
