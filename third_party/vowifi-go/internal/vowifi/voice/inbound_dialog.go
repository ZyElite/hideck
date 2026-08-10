package voice

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/iniwex5/vowifi-go/internal/vowifi/imscore"
	"github.com/iniwex5/vowifi-go/internal/vowifi/voice/callstate"
)

func (a *Agent) prepareInboundVoiceDialog(call *Call, request imscore.InboundVoiceRequest) error {
	profile, err := a.registeredDialogProfile()
	if err != nil {
		return err
	}
	cseq, err := inboundVoiceCSeq(request.CSeq)
	if err != nil {
		return err
	}
	remoteURI := voiceHeaderURI(request.From)
	remoteTarget := voiceHeaderURI(request.Contact)
	if remoteTarget == "" {
		remoteTarget = remoteURI
	}
	call.setVoiceDialog(&voiceSIPDialog{
		localURI: voiceHeaderURI(request.To), remoteURI: remoteURI, remoteTarget: remoteTarget,
		contactURI: profile.ContactURI, localAddress: profile.LocalAddress, transport: profile.Transport,
		serviceRoute: splitVoiceHeaderValues(request.RecordRoute), securityVerify: profile.SecurityVerify,
		pani: profile.PANI, userAgent: profile.UserAgent, localTag: inboundLocalTag(call, request.Responder),
		remoteTag: voiceHeaderTag(request.From), cseq: cseq,
	})
	return nil
}

func (a *Agent) reserveInboundCall(request imscore.InboundVoiceRequest) (*Call, bool, error) {
	call := a.newInboundCall(request)
	if err := call.TransitionChecked(callstate.StateRinging); err != nil {
		return nil, false, errors.Join(err, releaseUnregisteredCall(call))
	}
	a.mu.Lock()
	if existing := a.calls[request.CallID]; existing != nil && !existing.IsTerminalState() {
		a.mu.Unlock()
		return existing, false, releaseUnregisteredCall(call)
	}
	if a.activeCall != nil && !a.activeCall.IsTerminalState() {
		a.mu.Unlock()
		return nil, false, errors.Join(errors.New("voice: busy"), releaseUnregisteredCall(call))
	}
	a.calls[request.CallID] = call
	a.activeCall = call
	a.mu.Unlock()
	return call, true, nil
}

func (a *Agent) newInboundCall(request imscore.InboundVoiceRequest) *Call {
	if request.Request == nil {
		call := NewCall(a, callstate.DirectionInbound, request.CallID, voiceHeaderURI(request.From))
		call.callee = voiceHeaderURI(request.To)
		return call
	}
	call := NewCallFromRequest(a.deviceID, request.Request, request.Session)
	call.agent = a
	if call.callID == "" {
		call.callID = request.CallID
		call.DialogState.CallID = request.CallID
	}
	return call
}

func inboundLocalTag(call *Call, responder imscore.InboundVoiceResponder) string {
	tag := ""
	if responder != nil {
		tag = strings.TrimSpace(responder.LocalTag())
	}
	call.mu.Lock()
	defer call.mu.Unlock()
	if tag != "" {
		call.DialogState.ToTag = tag
	}
	if strings.TrimSpace(call.DialogState.ToTag) == "" {
		call.DialogState.ToTag = voiceTag()
	}
	return call.DialogState.ToTag
}

func inboundVoiceCSeq(value string) (int, error) {
	fields := strings.Fields(value)
	if len(fields) != 2 || !strings.EqualFold(fields[1], "INVITE") {
		return 0, fmt.Errorf("voice: invalid inbound INVITE CSeq %q", value)
	}
	valueInt, err := strconv.Atoi(fields[0])
	if err != nil || valueInt < 1 {
		return 0, fmt.Errorf("voice: invalid inbound INVITE CSeq %q", value)
	}
	return valueInt, nil
}
