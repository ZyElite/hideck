package ussi

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/emiago/sipgo/sip"
	"github.com/iniwex5/vowifi-go/internal/vowifi/imsendpoint"
)

const (
	menuText  = "1. Balance\n2. Data"
	finalText = "Balance: 10"
)

func TestRecoveredTypeLayouts(t *testing.T) {
	assertFields(t, reflect.TypeOf(Context{}), []string{
		"LocalIP", "LocalPortC", "LocalPortS", "Transport", "Domain", "Realm", "AOR",
		"RouteHeader", "ServiceRoute", "SecVerify", "Mode", "PANI", "UserAgent",
		"ContactID", "Destination",
	})
	assertFields(t, reflect.TypeOf(InfoResult{}), []string{"Text", "RawXML", "Err"})
	assertFields(t, reflect.TypeOf(Result{}), []string{"Text", "Status", "SessionID", "RawXML", "DCS"})
	assertFields(t, reflect.TypeOf(XMLPayload{}), []string{"XMLName", "Xmlns", "Language", "USSDString"})
	assertFields(t, reflect.TypeOf(Session{}), []string{
		"mu", "ID", "CallID", "RemoteURI", "RemoteTarget", "State", "ResultCh",
		"CreatedAt", "LastAt", "dialogContext", "dialogHandle",
	})
}

func TestEncodeDecodeXML(t *testing.T) {
	body, err := EncodeXML("*100#", "")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(body), `<?xml version="1.0" encoding="UTF-8"?>`) {
		t.Fatalf("XML header missing: %q", body)
	}
	decoded, err := DecodeXML(body)
	if err != nil {
		t.Fatal(err)
	}
	if decoded.USSDString != "*100#" || decoded.Language != "en" || decoded.Xmlns != ContentType {
		t.Fatalf("decoded = %+v", decoded)
	}
}

func TestParseResultMatchesRecoveredStatuses(t *testing.T) {
	menu := ParseResult(xmlBody(t, menuText), "ussd-1")
	if menu.Status != 1 || menu.SessionID != "ussd-1" || menu.DCS != 15 {
		t.Fatalf("menu = %+v", menu)
	}
	final := ParseResult(xmlBody(t, finalText), "ussd-1")
	if final.Status != 0 || final.SessionID != "" || final.Text != finalText {
		t.Fatalf("final = %+v", final)
	}
	malformed := ParseResult([]byte("not XML"), "ussd-1")
	if malformed.Text != "not XML" || malformed.Status != 0 {
		t.Fatalf("malformed fallback = %+v", malformed)
	}
	empty := ParseResult(nil, "ussd-1")
	if empty.Text != "(空响应)" || empty.DCS != 15 {
		t.Fatalf("empty = %+v", empty)
	}
}

func TestBuildInitialInviteRestoresWireProfile(t *testing.T) {
	ctx := testContext()
	ctx.RouteHeader = ctx.ServiceRoute
	body := xmlBody(t, "*100#")
	request, err := BuildInitialInvite(ctx, "*100#", "call-1", "tag-1", "z9hG4bKbranch", 7, body)
	if err != nil {
		t.Fatal(err)
	}
	if request.Method != sip.INVITE || !strings.Contains(request.Recipient.String(), "%23;phone-context=") {
		t.Fatalf("recipient = %s", request.Recipient.String())
	}
	for _, header := range []string{
		"Recv-Info", "P-Preferred-Service", "Accept-Contact", "P-Early-Media",
		"Require", "Proxy-Require", "Security-Verify",
	} {
		if request.GetHeader(header) == nil {
			t.Errorf("missing %s", header)
		}
	}
	if request.Contact() == nil || !strings.Contains(request.Contact().Value(), "+g.3gpp.nw-init-ussi") {
		t.Fatalf("Contact = %v", request.Contact())
	}
	extracted := ExtractFromMultipart(request.Body())
	if string(extracted) != string(body) {
		t.Fatalf("multipart XML = %q", extracted)
	}
	if !strings.Contains(string(request.Body()), "a=rtpmap:106 EVS/16000/1") {
		t.Fatal("recovered SDP capability set is missing")
	}
	if routes := request.GetHeaders("Route"); len(routes) != 1 {
		t.Fatalf("Route headers = %d, want 1", len(routes))
	}
}

func TestSplitLocalAddrHandlesIPv6WithPort(t *testing.T) {
	host, port := splitLocalAddr("[2001:db8::10]:5062", 5070)
	if host != "2001:db8::10" || port != 5062 {
		t.Fatalf("split address = %q:%d", host, port)
	}
}

func TestRecoveredContentTypeAndTransportFallbacks(t *testing.T) {
	if !IsContentType("application/3gpp-ussd+xml; charset=utf-8") {
		t.Fatal("legacy USSI content type was rejected")
	}
	if normalizedTransport("udp") != "UDP" || normalizedTransport("unknown") != "TCP" {
		t.Fatal("recovered transport normalization changed")
	}
}

func TestDialogRequestURIFallsBackToContextDomain(t *testing.T) {
	uri, err := dialogRequestURI(&Session{}, Context{Domain: "ims.example"})
	if err != nil {
		t.Fatal(err)
	}
	if uri.String() != "sip:ims.example" {
		t.Fatalf("dialog URI = %q", uri.String())
	}
}

func TestRecoveredSIPLogMetadata(t *testing.T) {
	request, err := BuildInitialInvite(
		testContext(), "*100#", "log-call", "tag", "z9hG4bKlog", 7, xmlBody(t, "*100#"),
	)
	if err != nil {
		t.Fatal(err)
	}
	method, callID := ussiSIPRawLogMethodAndCallID(request)
	if method != "INVITE" || callID != "log-call" {
		t.Fatalf("request metadata = %q %q", method, callID)
	}
	response := responseWithBody(request, nil)
	method, callID = ussiSIPRawLogMethodAndCallID(response)
	if method != "INVITE" || callID != "log-call" {
		t.Fatalf("response metadata = %q %q", method, callID)
	}
}

func TestClearSessionEmptyIDClearsCurrent(t *testing.T) {
	service := NewService("dev-1", newFakeEndpoint(t))
	session := &Session{ID: "active", State: sessionActive}
	service.setSession(session)
	if cleared := service.clearSession(""); cleared != session {
		t.Fatalf("cleared session = %p, want %p", cleared, session)
	}
	if service.ActiveSessionID() != "" || session.IsActive() {
		t.Fatal("empty-ID clear left the session active")
	}
}

func TestServiceSendContinueAndCancel(t *testing.T) {
	endpoint := newFakeEndpoint(t)
	endpoint.inviteBody = xmlBody(t, menuText)
	endpoint.infoBody = xmlBody(t, finalText)
	service := NewService("dev-1", endpoint)

	initial, err := service.Send(context.Background(), "*100#")
	if err != nil {
		t.Fatal(err)
	}
	if initial.Status != 1 || service.ActiveSessionID() != initial.SessionID {
		t.Fatalf("initial = %+v active=%q", initial, service.ActiveSessionID())
	}
	final, err := service.Continue(context.Background(), initial.SessionID, "1")
	if err != nil {
		t.Fatal(err)
	}
	if final.Status != 0 || final.Text != finalText || service.ActiveSessionID() != "" {
		t.Fatalf("final = %+v active=%q", final, service.ActiveSessionID())
	}

	endpoint.inviteBody = xmlBody(t, menuText)
	second, err := service.Send(context.Background(), "*101#")
	if err != nil {
		t.Fatal(err)
	}
	if err := service.Cancel(context.Background(), second.SessionID); err != nil {
		t.Fatal(err)
	}
	if got := endpoint.methods(); !reflect.DeepEqual(got, []sip.RequestMethod{sip.ACK, sip.INFO, sip.ACK, sip.BYE}) {
		t.Fatalf("dialog methods = %v", got)
	}
}

func TestServiceWaitsForInboundInfo(t *testing.T) {
	endpoint := newFakeEndpoint(t)
	service := NewService("dev-1", endpoint)
	resultCh := make(chan *Result, 1)
	errCh := make(chan error, 1)
	go func() {
		result, err := service.Send(context.Background(), "*100#")
		resultCh <- result
		errCh <- err
	}()
	request := <-endpoint.started
	inbound := sip.NewRequest(sip.INFO, *request.Recipient.Clone())
	callID := sip.CallIDHeader(request.CallID().Value())
	inbound.AppendHeader(&callID)
	inbound.AppendHeader(sip.NewHeader("Content-Type", ContentType))
	inbound.SetBody(xmlBody(t, menuText))
	if !service.HandleInboundInfoNoResponse(context.Background(), inbound) {
		t.Fatal("inbound INFO was not consumed")
	}
	if err := <-errCh; err != nil {
		t.Fatal(err)
	}
	if result := <-resultCh; result == nil || result.Status != 1 {
		t.Fatalf("result = %+v", result)
	}
}

func TestServiceSerializesConcurrentSends(t *testing.T) {
	endpoint := newFakeEndpoint(t)
	endpoint.inviteBody = xmlBody(t, finalText)
	gate := make(chan struct{})
	endpoint.inviteGate = gate
	service := NewService("dev-1", endpoint)
	first := make(chan error, 1)
	go func() {
		_, err := service.Send(context.Background(), "*100#")
		first <- err
	}()
	<-endpoint.started

	if _, err := service.Send(context.Background(), "*101#"); err == nil || !strings.Contains(err.Error(), "已有活动") {
		t.Fatalf("concurrent Send error = %v", err)
	}
	close(gate)
	if err := <-first; err != nil {
		t.Fatal(err)
	}
}

func TestServiceTimeoutAndStopCleanDialogs(t *testing.T) {
	endpoint := newFakeEndpoint(t)
	service := NewService("dev-1", endpoint)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	result, err := service.Send(ctx, "*100#")
	if !errors.Is(err, context.DeadlineExceeded) || result == nil || result.Status != 5 {
		t.Fatalf("timeout result=%+v err=%v", result, err)
	}
	if service.ActiveSessionID() != "" || endpoint.closeCount() != 1 {
		t.Fatalf("timeout cleanup active=%q closes=%d", service.ActiveSessionID(), endpoint.closeCount())
	}
	<-endpoint.started

	blocked := make(chan error, 1)
	go func() {
		_, sendErr := service.Send(context.Background(), "*101#")
		blocked <- sendErr
	}()
	<-endpoint.started
	service.Stop()
	select {
	case stopErr := <-blocked:
		if stopErr == nil || !strings.Contains(stopErr.Error(), "service stopped") {
			t.Fatalf("stop error = %v", stopErr)
		}
	case <-time.After(time.Second):
		t.Fatal("Stop did not wake Send")
	}
}

func assertFields(t *testing.T, typ reflect.Type, want []string) {
	t.Helper()
	if typ.NumField() != len(want) {
		t.Fatalf("%s fields = %d, want %d", typ, typ.NumField(), len(want))
	}
	for index, name := range want {
		if typ.Field(index).Name != name {
			t.Fatalf("%s field %d = %s, want %s", typ, index, typ.Field(index).Name, name)
		}
	}
}

func testContext() Context {
	return Context{
		LocalIP: "192.0.2.10", LocalPortC: 5062, LocalPortS: 5064,
		Transport: "udp", Domain: "ims.example", Realm: "ims.example",
		AOR: "sip:+15551234567@ims.example", ServiceRoute: "<sip:pcscf.ims.example;lr>",
		SecVerify: "ipsec-3gpp;alg=hmac-md5-96", Mode: "ipsec-3gpp",
		PANI: "IEEE-802.11", UserAgent: "ussi-test", ContactID: "contact-1",
	}
}

func xmlBody(t *testing.T, text string) []byte {
	t.Helper()
	body, err := EncodeXML(text, "en")
	if err != nil {
		t.Fatal(err)
	}
	return body
}

type fakeDialog struct{ id string }

func (d fakeDialog) DialogID() string { return d.id }

type fakeEndpoint struct {
	t             *testing.T
	mu            sync.Mutex
	cseq          uint32
	inviteBody    []byte
	infoBody      []byte
	dialogMethods []sip.RequestMethod
	closed        int
	started       chan *sip.Request
	inviteGate    <-chan struct{}
}

func newFakeEndpoint(t *testing.T) *fakeEndpoint {
	return &fakeEndpoint{t: t, cseq: 6, started: make(chan *sip.Request, 4)}
}

func (e *fakeEndpoint) DeviceID() string   { return "dev-1" }
func (e *fakeEndpoint) IsRegistered() bool { return true }
func (e *fakeEndpoint) NextCSeq() uint32 {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.cseq++
	return e.cseq
}
func (e *fakeEndpoint) Snapshot() imsendpoint.Snapshot {
	ctx := testContext()
	return imsendpoint.Snapshot{
		IMPU: ctx.AOR, Realm: ctx.Realm, ContactID: ctx.ContactID,
		ServiceRoute: ctx.ServiceRoute, SecVerify: ctx.SecVerify,
		EffectiveSecMode: ctx.Mode, PAccessNetworkInfo: ctx.PANI,
		UserAgent: ctx.UserAgent, LocalAddr: ctx.LocalIP,
		LocalPortC: ctx.LocalPortC, LocalPortS: ctx.LocalPortS, Transport: ctx.Transport,
	}
}
func (e *fakeEndpoint) StartClientInvite(
	_ context.Context,
	_ string,
	options imsendpoint.ClientInviteOptions,
) (*imsendpoint.ClientInviteResult, error) {
	e.started <- options.Request.Clone()
	if e.inviteGate != nil {
		<-e.inviteGate
	}
	response := responseWithBody(options.Request, e.inviteBody)
	return &imsendpoint.ClientInviteResult{
		Dialog: fakeDialog{id: options.Request.CallID().Value()}, Response: response,
	}, nil
}
func (e *fakeEndpoint) SendDialogRequest(
	_ context.Context,
	_ string,
	_ imsendpoint.DialogHandle,
	request *sip.Request,
	_ imsendpoint.DialogRequestOptions,
) (*sip.Response, error) {
	e.mu.Lock()
	e.dialogMethods = append(e.dialogMethods, request.Method)
	body := append([]byte(nil), e.infoBody...)
	e.mu.Unlock()
	if request.Method == sip.ACK {
		return nil, nil
	}
	return responseWithBody(request, body), nil
}
func (e *fakeEndpoint) CloseDialog(context.Context, string, imsendpoint.DialogHandle) error {
	e.mu.Lock()
	e.closed++
	e.mu.Unlock()
	return nil
}
func (e *fakeEndpoint) methods() []sip.RequestMethod {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]sip.RequestMethod(nil), e.dialogMethods...)
}
func (e *fakeEndpoint) closeCount() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.closed
}

func responseWithBody(request *sip.Request, body []byte) *sip.Response {
	response := sip.NewResponseFromRequest(request, 200, "OK", body)
	if len(body) > 0 {
		response.AppendHeader(sip.NewHeader("Content-Type", ContentType))
	}
	return response
}

func (*fakeEndpoint) AnswerServerInvite(
	context.Context, string, imsendpoint.ServerInviteHandle, imsendpoint.ServerInviteAnswerOptions,
) (imsendpoint.DialogHandle, error) {
	return nil, errors.New("unused")
}
func (*fakeEndpoint) CancelClientInvite(
	context.Context, string, imsendpoint.InviteHandle, imsendpoint.ClientInviteCancelOptions,
) error {
	return errors.New("unused")
}
func (*fakeEndpoint) RejectServerInvite(
	context.Context, string, imsendpoint.ServerInviteHandle, imsendpoint.ServerInviteRejectOptions,
) error {
	return errors.New("unused")
}
func (*fakeEndpoint) SendReliableProvisionalPRACK(
	context.Context, string, imsendpoint.ReliableProvisionalOptions,
) error {
	return errors.New("unused")
}
