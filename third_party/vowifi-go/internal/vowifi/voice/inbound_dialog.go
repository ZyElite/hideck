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
	profile, err := a.ims.RegisteredSIPDialogProfile()
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
		pani: profile.PANI, userAgent: profile.UserAgent, localTag: request.Responder.LocalTag(),
		remoteTag: voiceHeaderTag(request.From), cseq: cseq,
	})
	return nil
}

func (a *Agent) reserveInboundCall(request imscore.InboundVoiceRequest) (*Call, error) {
	call := NewCall(a, callstate.DirectionInbound, request.CallID, voiceHeaderURI(request.From))
	call.callee = voiceHeaderURI(request.To)
	if err := call.Transition(callstate.StateAlerting); err != nil {
		return nil, err
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.activeCall != nil && !a.activeCall.IsTerminalState() {
		return nil, errors.New("voice: busy")
	}
	a.calls[request.CallID] = call
	a.activeCall = call
	return call, nil
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
