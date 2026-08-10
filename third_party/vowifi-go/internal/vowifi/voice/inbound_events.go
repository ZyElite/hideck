package voice

import (
	"context"
	"errors"
	"strings"

	"github.com/emiago/sipgo/sip"
	"github.com/iniwex5/vowifi-go/internal/vowifi/imscore"
	"github.com/iniwex5/vowifi-go/internal/vowifi/imsendpoint"
	"github.com/iniwex5/vowifi-go/internal/vowifi/logging"
	"github.com/iniwex5/vowifi-go/internal/vowifi/voice/callstate"
)

const (
	voiceIMSEventSubscription = "voice_ims_dispatch"
	voiceIMSEventQueueSize    = 64
)

func (a *Agent) subscribeIMSEvents() func() {
	if a == nil || a.ims == nil {
		return nil
	}
	return a.ims.Subscribe(imsendpoint.EventSubscription{
		Name: voiceIMSEventSubscription, QueueSize: voiceIMSEventQueueSize,
		Workers: 1, Match: isVoiceIMSEvent,
	}, a.handleIMSEvent)
}

func isVoiceIMSEvent(event imsendpoint.Event) bool {
	method := strings.ToUpper(strings.TrimSpace(event.Method))
	if method == "" {
		method = strings.ToUpper(strings.TrimSpace(event.CSeqMethod))
	}
	switch strings.ToLower(strings.TrimSpace(event.Kind)) {
	case "request":
		return method == "INVITE" || method == "BYE" || method == "CANCEL" || method == "UPDATE"
	case "response":
		return method == "INVITE" || method == "PRACK"
	default:
		return false
	}
}

// OwnsInboundVoiceMethod prevents the synchronous compatibility router from
// consuming the same request as the restored endpoint event path.
func (a *Agent) OwnsInboundVoiceMethod(method string) bool {
	switch strings.ToUpper(strings.TrimSpace(method)) {
	case "INVITE", "BYE", "CANCEL", "UPDATE":
		return true
	default:
		return false
	}
}

// InboundVoiceEventSubscription identifies the queue that must accept an
// event before imscore transfers ownership from its synchronous router.
func (a *Agent) InboundVoiceEventSubscription() string {
	return voiceIMSEventSubscription
}

func (a *Agent) handleIMSEvent(event imsendpoint.Event) {
	kind := strings.ToLower(strings.TrimSpace(event.Kind))
	if kind == "response" {
		a.handleIMSResponseEvent(event)
		return
	}
	if kind != "request" {
		return
	}
	switch strings.ToUpper(strings.TrimSpace(event.Method)) {
	case "INVITE":
		a.OnIMSInvite(event.Request, event.Session, event.ServerInvite)
	case "BYE":
		a.HandleIMSByeEvent(event)
	case "CANCEL":
		a.HandleIMSCancelEvent(event)
	case "UPDATE":
		a.HandleIMSUpdateEvent(event)
	}
}

// OnIMSInvite retains the v1.5.5 inbound server-INVITE entry point.
func (a *Agent) OnIMSInvite(
	request *sip.Request,
	session *imsendpoint.Session,
	handle imsendpoint.ServerInviteHandle,
) {
	if a == nil || request == nil {
		logging.WarnRate("voice-inbound-invite-nil", voiceActorEventLogInterval,
			"voice inbound INVITE is nil")
		return
	}
	method := strings.ToUpper(strings.TrimSpace(string(request.Method)))
	if method == "CANCEL" {
		a.HandleIMSCancelEvent(imsendpoint.Event{
			Kind: "request", Method: method, CallID: requestCallID(request),
			Session: session, Request: request, ServerInvite: handle,
		})
		return
	}
	if method != "" && method != "INVITE" {
		return
	}
	inbound := inboundRequestFromEvent(request, session, handle)
	result, err := a.HandleInboundVoiceRequest(inbound)
	if err != nil {
		logging.WarnRate("voice-inbound-invite:"+inbound.CallID, voiceActorEventLogInterval,
			"voice inbound INVITE failed", "device", a.deviceID, "call_id", inbound.CallID, "err", err)
	}
	if result.StatusCode != 0 {
		if result.StatusCode == 486 {
			a.rejectBusy(request, session, handle)
			return
		}
		a.rejectUnownedServerInvite(request, session, handle, result.StatusCode)
	}
}

func inboundRequestFromEvent(
	request *sip.Request,
	session *imsendpoint.Session,
	handle imsendpoint.ServerInviteHandle,
) imscore.InboundVoiceRequest {
	return imscore.InboundVoiceRequest{
		Method: string(request.Method), CallID: requestCallID(request),
		From: requestHeaderValue(request, "From"), To: requestHeaderValue(request, "To"),
		Contact:        requestHeaderValue(request, "Contact"),
		RecordRoute:    joinedRequestHeaders(request, "Record-Route"),
		CSeq:           requestHeaderValue(request, "CSeq"),
		ContentType:    requestHeaderValue(request, "Content-Type"),
		SessionExpires: requestHeaderValue(request, "Session-Expires"),
		Body:           append([]byte(nil), request.Body()...), Request: request.Clone(),
		ServerInvite: handle, Session: session,
	}
}

func (a *Agent) inboundRequestFromEndpointEvent(event imsendpoint.Event) imscore.InboundVoiceRequest {
	request := event.Request
	if request == nil {
		return imscore.InboundVoiceRequest{Method: event.Method, CallID: event.CallID}
	}
	inbound := inboundRequestFromEvent(request, event.Session, event.ServerInvite)
	inbound.InboundRequest = event.InboundRequest
	if event.InboundRequest != nil {
		inbound.Responder = a.newEndpointEventResponder(event)
	}
	return inbound
}

func (a *Agent) newEndpointEventResponder(event imsendpoint.Event) imscore.InboundVoiceResponder {
	localTag := ""
	if call := a.callForIMSEvent(event); call != nil {
		call.mu.RLock()
		localTag = call.DialogState.ToTag
		call.mu.RUnlock()
	}
	return &endpointEventResponder{
		agent: a, handle: event.InboundRequest, localTag: localTag,
	}
}

type endpointEventResponder struct {
	agent    *Agent
	handle   imsendpoint.InboundRequestHandle
	localTag string
}

func (r *endpointEventResponder) LocalTag() string {
	if r == nil {
		return ""
	}
	return r.localTag
}

func (r *endpointEventResponder) Respond(response imscore.InboundVoiceResponse) error {
	if r == nil || r.agent == nil || r.agent.ims == nil || r.handle == nil {
		return errors.New("voice: IMS inbound response path is unavailable")
	}
	headers := endpointResponseHeaders(response)
	return r.agent.ims.RespondInboundRequest(context.Background(), r.agent.deviceID, r.handle,
		imsendpoint.InboundResponseOptions{
			Code: response.StatusCode, Body: append([]byte(nil), response.Body...), Headers: headers,
		})
}

func endpointResponseHeaders(response imscore.InboundVoiceResponse) []sip.Header {
	headers := make([]sip.Header, 0, 3)
	if value := strings.TrimSpace(response.ContentType); value != "" {
		headers = append(headers, sip.NewHeader("Content-Type", value))
	}
	if value := strings.TrimSpace(response.Contact); value != "" {
		headers = append(headers, sip.NewHeader("Contact", "<"+strings.Trim(value, "<>")+">"))
	}
	if value := strings.TrimSpace(response.SessionExpires); value != "" {
		headers = append(headers, sip.NewHeader("Session-Expires", value))
	}
	return headers
}

func joinedRequestHeaders(request *sip.Request, name string) string {
	if request == nil {
		return ""
	}
	values := make([]string, 0, len(request.GetHeaders(name)))
	for _, header := range request.GetHeaders(name) {
		if value := strings.TrimSpace(header.Value()); value != "" {
			values = append(values, value)
		}
	}
	return strings.Join(values, ", ")
}

func (a *Agent) rejectUnownedServerInvite(
	request *sip.Request,
	session *imsendpoint.Session,
	handle imsendpoint.ServerInviteHandle,
	status int,
) {
	if a == nil || a.dialog == nil || request == nil || handle == nil {
		return
	}
	call := NewCallFromRequest(a.deviceID, request, session)
	defer call.CloseDone()
	response := call.BuildResponse(status, imscore.SIPStatusText(status))
	err := a.dialog.RejectServerInvite(context.Background(), a.deviceID, handle,
		imsendpoint.ServerInviteRejectOptions{Response: response})
	if err != nil {
		logging.WarnRate("voice-inbound-reject:"+requestCallID(request), voiceActorEventLogInterval,
			"voice inbound INVITE rejection failed", "device", a.deviceID,
			"call_id", requestCallID(request), "status", status, "err", err)
	}
}

// HandleIMSByeEvent retains the v1.5.5 endpoint event entry point.
func (a *Agent) HandleIMSByeEvent(event imsendpoint.Event) {
	a.handleIMSRequestEvent(event)
}

// HandleIMSCancelEvent retains the v1.5.5 endpoint event entry point.
func (a *Agent) HandleIMSCancelEvent(event imsendpoint.Event) {
	a.handleIMSRequestEvent(event)
}

// HandleIMSUpdateEvent retains the v1.5.5 endpoint event entry point.
func (a *Agent) HandleIMSUpdateEvent(event imsendpoint.Event) {
	a.handleIMSRequestEvent(event)
}

func (a *Agent) handleIMSRequestEvent(event imsendpoint.Event) {
	inbound := a.inboundRequestFromEndpointEvent(event)
	responded, responseErr := a.respondBeforeIMSRequest(event, inbound)
	result, handlerErr := a.HandleInboundVoiceRequest(inbound)
	err := errors.Join(responseErr, handlerErr)
	if err != nil {
		logging.WarnRate("voice-ims-request:"+inbound.CallID+":"+inbound.Method, voiceActorEventLogInterval,
			"voice IMS request failed", "device", a.deviceID, "call_id", inbound.CallID,
			"method", inbound.Method, "err", err)
	}
	if responded || result.StatusCode == 0 || inbound.Responder == nil {
		return
	}
	if err := inbound.Responder.Respond(imscore.InboundVoiceResponse{StatusCode: result.StatusCode}); err != nil {
		logging.WarnRate("voice-ims-response:"+inbound.CallID+":"+inbound.Method, voiceActorEventLogInterval,
			"voice IMS response failed", "device", a.deviceID, "call_id", inbound.CallID,
			"method", inbound.Method, "status", result.StatusCode, "err", err)
	}
}

func (a *Agent) respondBeforeIMSRequest(
	event imsendpoint.Event,
	inbound imscore.InboundVoiceRequest,
) (bool, error) {
	if !strings.EqualFold(strings.TrimSpace(inbound.Method), "BYE") || inbound.Responder == nil {
		return false, nil
	}
	call := a.callForIMSEvent(event)
	if call == nil || call.CallState() != callstate.StateConnected {
		return false, nil
	}
	return true, inbound.Responder.Respond(imscore.InboundVoiceResponse{StatusCode: 200})
}

func (a *Agent) callForIMSEvent(event imsendpoint.Event) *Call {
	if a == nil {
		return nil
	}
	callID := strings.TrimSpace(event.CallID)
	if callID == "" && event.Request != nil {
		callID = requestCallID(event.Request)
	}
	if call := a.callByID(callID); call != nil {
		return call
	}
	a.mu.RLock()
	active := a.activeCall
	a.mu.RUnlock()
	if callMatchesID(active, callID) {
		return active
	}
	return nil
}
