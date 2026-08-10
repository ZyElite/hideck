package voice

import (
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/emiago/sipgo/sip"
	"github.com/iniwex5/vowifi-go/internal/vowifi/events"
	"github.com/iniwex5/vowifi-go/internal/vowifi/imsendpoint"
	"github.com/iniwex5/vowifi-go/internal/vowifi/voice/callstate"
)

type originalCallModelAPI interface {
	GetState() int
	Transition(int) bool
	MarkInviteProvisional(int)
	MarkLocalCancelSent(string) bool
	MarkReliableProvisional(uint32) bool
	MarkInviteFinalSeen() bool
	MarkErrorACKSent() bool
	SetIMSDialog(imsendpoint.DialogHandle)
	SetIMSInviteHandle(imsendpoint.InviteHandle)
	SetOutboundCancel(func())
	SetOutboundRuntimeCancel(func())
	BuildResponse(int, string) *sip.Response
	BuildResponseWithSDP(int, string, []byte) *sip.Response
	Duration() int64
	CloseDone()
}

var _ originalCallModelAPI = (*Call)(nil)

type restorationDialogHandle string

func (h restorationDialogHandle) DialogID() string { return string(h) }

type restorationInviteHandle string

func (h restorationInviteHandle) InviteID() string { return string(h) }

func cleanupRestorationCall(call *Call) func() {
	return func() {
		call.CloseDone()
		if call.Cancel != nil {
			call.Cancel()
		}
	}
}

func TestCallRetainsOriginalFieldPrefixAndRuntimeOwnership(t *testing.T) {
	wantFields := []string{
		"DeviceID", "Direction", "State", "TraceID", "Done", "doneOnce", "Ctx", "Cancel",
		"outboundRuntimeCancel", "outboundNoAnswerStop", "mu", "startTime", "endTime",
		"DialogState", "MediaState", "Timers", "actor",
	}
	wantTypes := []string{
		"string", "int", "int", "string", "chan struct {}", "sync.Once", "context.Context", "func()",
		"func()", "func()", "sync.RWMutex", "time.Time", "time.Time", "callstate.DialogState",
		"callstate.MediaState", "callstate.Timers", "*callstate.Actor",
	}
	typeOfCall := reflect.TypeOf(Call{})
	for index, want := range wantFields {
		field := typeOfCall.Field(index)
		if got := field.Name; got != want {
			t.Fatalf("Call field %d = %q, want %q", index, got, want)
		}
		if got := field.Type.String(); got != wantTypes[index] {
			t.Fatalf("Call field %q type = %q, want %q", want, got, wantTypes[index])
		}
	}
	agent := NewAgent("device-31", nil, nil)
	call := NewCall(agent, callstate.DirectionOutbound, "call-31", "43430")
	t.Cleanup(cleanupRestorationCall(call))
	if call.DeviceID != "device-31" || call.Direction != int(callstate.DirectionOutbound) || call.TraceID == "" {
		t.Fatalf("call identity = device=%q direction=%d trace=%q", call.DeviceID, call.Direction, call.TraceID)
	}
	if call.Ctx == nil || call.Cancel == nil || call.Done == nil || call.actor == nil {
		t.Fatal("call runtime ownership was not initialized")
	}
	if call.DialogState.CallID != "call-31" || call.DialogState.CalleeID != "43430" {
		t.Fatalf("dialog identity = %+v", call.DialogState)
	}
}

func TestOriginalCallConstructorsParseSIPIdentity(t *testing.T) {
	if callstate.DirectionInbound != 0 || callstate.DirectionOutbound != 1 {
		t.Fatalf("direction values inbound=%d outbound=%d", callstate.DirectionInbound, callstate.DirectionOutbound)
	}
	request := originalCallInvite(t)
	inbound := NewCallFromRequest("device-31", request, nil)
	t.Cleanup(cleanupRestorationCall(inbound))
	if inbound.DeviceID != "device-31" || inbound.Direction != 0 || inbound.DialogState.CallerID != "alice" {
		t.Fatalf("inbound identity = %+v", inbound.DialogState)
	}
	if inbound.DialogState.CallID != "legacy-call" || inbound.DialogState.IMSCallID != "legacy-call" || inbound.TraceID != "legacy-call" {
		t.Fatalf("inbound transaction = %+v trace=%q", inbound.DialogState, inbound.TraceID)
	}
	if inbound.Timers.SessionExpires != 1800 || !reflect.DeepEqual(inbound.DialogState.RouteSet, []string{
		"<sip:edge-three.example;lr>", "<sip:edge-two.example;lr>", "<sip:edge-one.example;lr>",
	}) {
		t.Fatalf("inbound routes=%v session_expires=%d", inbound.DialogState.RouteSet, inbound.Timers.SessionExpires)
	}
	outbound := NewOutboundCall("outbound-call", "alice", "bob")
	t.Cleanup(cleanupRestorationCall(outbound))
	if outbound.Direction != 1 || outbound.DialogState.CallerID != "alice" || outbound.DialogState.CalleeID != "bob" {
		t.Fatalf("outbound identity = %+v", outbound.DialogState)
	}
	emptyTrace := NewOutboundCall("  ", "alice", "bob")
	t.Cleanup(cleanupRestorationCall(emptyTrace))
	if emptyTrace.TraceID != "" {
		t.Fatalf("whitespace call ID trace = %q", emptyTrace.TraceID)
	}
	client := NewCallFromClientInvite("device-31", request)
	t.Cleanup(cleanupRestorationCall(client))
	if client.DialogState.ClientCallID != "legacy-call" || client.DialogState.ClientFromTag != "from-tag" {
		t.Fatalf("client identity = %+v", client.DialogState)
	}
	if inbound.DialogState.OriginalRequest == request || client.DialogState.OriginalRequest != request {
		t.Fatal("inbound request was not cloned or client request identity was not retained")
	}
}

func originalCallInvite(t *testing.T) *sip.Request {
	t.Helper()
	request := sip.NewRequest(sip.INVITE, sip.Uri{Scheme: "sip", User: "bob", Host: "ims.example.com"})
	fromParams := sip.NewParams()
	fromParams.Add("tag", "from-tag")
	viaParams := sip.NewParams()
	viaParams.Add("branch", "z9hG4bK-call-31")
	request.AppendHeader(&sip.FromHeader{Address: sip.Uri{Scheme: "sip", User: "alice", Host: "ims.example.com"}, Params: fromParams})
	request.AppendHeader(&sip.ToHeader{Address: sip.Uri{Scheme: "sip", User: "bob", Host: "ims.example.com"}, Params: sip.NewParams()})
	callID := sip.CallIDHeader("legacy-call")
	request.AppendHeader(&callID)
	request.AppendHeader(&sip.CSeqHeader{SeqNo: 42, MethodName: sip.INVITE})
	request.AppendHeader(&sip.ViaHeader{Transport: "UDP", Host: "127.0.0.1", Params: viaParams})
	request.AppendHeader(sip.NewHeader("Record-Route", "<sip:edge-one.example;lr>, <sip:edge-two.example;lr>"))
	request.AppendHeader(sip.NewHeader("Record-Route", "<sip:edge-three.example;lr>"))
	request.AppendHeader(sip.NewHeader("Session-Expires", "1800;refresher=uac"))
	request.SetBody([]byte("v=0\r\n"))
	return request
}

func TestExtractPhoneNumberMatchesRecoveredPatterns(t *testing.T) {
	tests := map[string]string{
		`<tel:+447700900123>`:                    "+447700900123",
		`<sip:85075@ims.example.com>`:            "85075",
		`<sip:alice;user=phone@ims.example.com>`: "alice",
		`<sips:alice@ims.example.com>`:           "",
	}
	for input, want := range tests {
		if got := extractPhoneNumber(input); got != want {
			t.Errorf("extractPhoneNumber(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestCallBuildResponseRetainsTransactionAndStableToTag(t *testing.T) {
	request := originalCallInvite(t)
	session := &imsendpoint.Session{LocalIP: "2001:db8::31", LocalPortC: 5062}
	call := NewCallFromRequest("device-31", request, session)
	t.Cleanup(cleanupRestorationCall(call))

	ringing := call.BuildResponse(180, "Ringing")
	ok := call.BuildResponseWithSDP(200, "OK", []byte("v=0\r\n"))
	assertResponseTransaction(t, ringing, request)
	assertResponseTransaction(t, ok, request)
	ringingTag := sipHeaderTag(ringing.To())
	if ringingTag == "" || sipHeaderTag(ok.To()) != ringingTag || call.DialogState.ToTag != ringingTag {
		t.Fatalf("stable To-tag ringing=%q ok=%q stored=%q", ringingTag, sipHeaderTag(ok.To()), call.DialogState.ToTag)
	}
	if got := responseHeaderValue(ok, "Supported"); got != "timer" {
		t.Fatalf("Supported = %q", got)
	}
	if got := responseHeaderValue(ok, "Content-Type"); got != "application/sdp" {
		t.Fatalf("Content-Type = %q", got)
	}
	if got := string(ok.Body()); got != "v=0\r\n" {
		t.Fatalf("SDP body = %q", got)
	}
	contact := ok.GetHeader("Contact")
	typed, valid := contact.(*sip.ContactHeader)
	if !valid || typed.Address.User != "bob" || typed.Address.Host != "2001:db8::31" || typed.Address.Port != 5062 {
		t.Fatalf("Contact = %#v", contact)
	}
}

func assertResponseTransaction(t *testing.T, response *sip.Response, request *sip.Request) {
	t.Helper()
	if response == nil || response.Via() == nil || response.From() == nil || response.CallID() == nil || response.CSeq() == nil {
		t.Fatalf("incomplete response transaction: %#v", response)
	}
	if response.Via().Value() != request.Via().Value() || response.From().Value() != request.From().Value() {
		t.Fatalf("response transaction differs: %s", response.String())
	}
	if response.CallID().Value() != request.CallID().Value() || response.CSeq().Value() != request.CSeq().Value() {
		t.Fatalf("response identifiers differ: %s", response.String())
	}
}

func responseHeaderValue(message sip.Message, name string) string {
	headers := message.GetHeaders(name)
	if len(headers) == 0 {
		return ""
	}
	return headers[0].Value()
}

func TestCallOriginalTransitionAndOneShotMarkers(t *testing.T) {
	call := NewCall(nil, callstate.DirectionOutbound, "call-31", "43430")
	t.Cleanup(cleanupRestorationCall(call))
	if !call.Transition(int(callstate.StateDialing)) || !call.Transition(int(callstate.StateDialing)) {
		t.Fatal("valid and same-state transitions must succeed")
	}
	if call.Transition(int(callstate.StateConnected)) {
		t.Fatal("invalid transition unexpectedly succeeded")
	}
	if err := call.TransitionChecked(callstate.StateConnected); err == nil {
		t.Fatal("additive checked transition did not expose the invalid edge")
	}
	if !call.MarkInviteFinalSeen() || call.MarkInviteFinalSeen() {
		t.Fatal("final response marker is not one-shot")
	}
	if call.GetState() != int(callstate.StateEnded) {
		t.Fatalf("final response state = %d", call.GetState())
	}
	canceled := NewCall(nil, callstate.DirectionOutbound, "cancel-31", "43430")
	t.Cleanup(cleanupRestorationCall(canceled))
	_ = canceled.Transition(int(callstate.StateDialing))
	if !canceled.MarkLocalCancelSent(" local_cancel ") || canceled.MarkLocalCancelSent("duplicate") {
		t.Fatal("local cancel marker is not one-shot")
	}
	if canceled.LocalCancelReasonValue() != "local_cancel" || canceled.CallState() != callstate.StateFailed {
		t.Fatalf("cancel reason = %q state=%s", canceled.LocalCancelReasonValue(), canceled.CallState())
	}
	if !call.MarkErrorACKSent() || call.MarkErrorACKSent() {
		t.Fatal("error ACK marker is not one-shot")
	}
	provisional := NewCall(nil, callstate.DirectionOutbound, "provisional-31", "43430")
	t.Cleanup(cleanupRestorationCall(provisional))
	provisional.MarkInviteProvisional(180)
	if provisional.CallState() != callstate.StateAlerting {
		t.Fatalf("180 state = %s", provisional.CallState())
	}
	provisional.MarkInviteProvisional(183)
	if provisional.CallState() != callstate.StateConnecting {
		t.Fatalf("183 state = %s", provisional.CallState())
	}
	if !canceled.MarkReliableProvisional(10) || canceled.MarkReliableProvisional(10) ||
		!canceled.MarkReliableProvisional(11) || !canceled.MarkReliableProvisional(10) {
		t.Fatal("reliable provisional marker did not deduplicate only the latest RSeq")
	}
}

func TestCallRetainsRecoveredHandleAndCancelInterfaces(t *testing.T) {
	call := NewCall(nil, callstate.DirectionOutbound, "call-interfaces", "43430")
	t.Cleanup(cleanupRestorationCall(call))
	dialog := restorationDialogHandle("dialog-31")
	invite := restorationInviteHandle("invite-31")
	call.SetIMSDialog(dialog)
	call.SetIMSInviteHandle(invite)
	if call.IMSDialogValue() != dialog || call.IMSInviteHandleValue() != invite {
		t.Fatalf("handles dialog=%v invite=%v", call.IMSDialogValue(), call.IMSInviteHandleValue())
	}
	var canceled bool
	call.SetOutboundCancel(func() { canceled = true })
	call.mu.RLock()
	cancel := call.outboundRuntimeCancel
	call.mu.RUnlock()
	if cancel == nil {
		t.Fatal("outbound cancel callback was not retained")
	}
	cancel()
	if !canceled {
		t.Fatal("outbound cancel callback did not run")
	}
}

func TestCallActorSerializesProductionEvents(t *testing.T) {
	const eventCount = 32
	agent := NewAgent("device-31", nil, nil)
	call := NewCall(agent, callstate.DirectionOutbound, "call-31", "43430")
	agent.mu.Lock()
	agent.calls[call.CallID()] = call
	agent.mu.Unlock()
	var active, maximum, completed atomic.Int32
	done := make(chan struct{})
	agent.SetNotifier(func(events.Event) {
		current := active.Add(1)
		updateActorMaximum(&maximum, current)
		time.Sleep(time.Millisecond)
		active.Add(-1)
		if completed.Add(1) == eventCount {
			close(done)
		}
	})
	var producers sync.WaitGroup
	producers.Add(eventCount)
	for range eventCount {
		go func() {
			defer producers.Done()
			agent.emitCallRinging(call)
		}()
	}
	producers.Wait()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatalf("received %d of %d events", completed.Load(), eventCount)
	}
	if maximum.Load() != 1 {
		t.Fatalf("maximum concurrent call notifier executions = %d", maximum.Load())
	}
	call.CloseDone()
	select {
	case <-call.Ctx.Done():
		t.Fatal("CloseDone canceled the independently owned call context")
	default:
	}
	call.Cancel()
	select {
	case <-call.Ctx.Done():
	case <-time.After(time.Second):
		t.Fatal("call context remained active after Cancel")
	}
}
