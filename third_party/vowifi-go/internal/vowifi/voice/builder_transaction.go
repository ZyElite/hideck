package voice

import (
	"errors"
	"fmt"
	"strings"

	"github.com/emiago/sipgo/sip"
	"github.com/iniwex5/vowifi-go/internal/vowifi/sipkit"
)

const (
	voiceInviteSupported = "100rel, timer, replaces, norefersub, early-session, sec-agree"
	voiceInviteAllow     = "INVITE, ACK, CANCEL, BYE, UPDATE, REFER, NOTIFY, MESSAGE, OPTIONS"
)

// BuildIMSInvite builds the initial IMS INVITE with the registered route.
func BuildIMSInvite(agent *Agent, call *Call) string {
	return buildIMSInviteWithSDP(agent, call, "")
}

func buildIMSInviteWithSDP(agent *Agent, call *Call, sdp string) string {
	request, _ := buildIMSInviteWithSDPChecked(agent, call, sdp)
	return request
}

func buildIMSInviteWithSDPChecked(agent *Agent, call *Call, sdp string) (string, error) {
	if agent == nil || call == nil {
		return "", errors.New("voice: missing agent or call")
	}
	dialog := call.voiceDialogSnapshot()
	if dialog.remoteURI == "" {
		dialog = fallbackVoiceDialog(agent, call)
		call.setVoiceDialog(&dialog)
	}
	if strings.TrimSpace(sdp) == "" {
		sdp = generateBasicSDP(agent, call)
	}
	recipient, err := parseVoiceURI(dialog.remoteURI)
	if err != nil {
		return "", fmt.Errorf("voice: parse INVITE target: %w", err)
	}
	from, err := parseVoiceURI(dialog.localURI)
	if err != nil {
		return "", fmt.Errorf("voice: parse INVITE identity: %w", err)
	}
	options := voiceIMSRequestOptions(dialog, voiceInitialRequest{
		callID: call.CallID(), from: from, to: recipient, sdp: sdp,
	})
	request, err := sipkit.BuildIMSRequest(sip.INVITE, recipient, options)
	if err != nil {
		return "", fmt.Errorf("voice: build IMS INVITE: %w", err)
	}
	return request.String(), nil
}

type voiceInitialRequest struct {
	callID string
	from   sip.Uri
	to     sip.Uri
	sdp    string
}

func voiceIMSRequestOptions(dialog voiceSIPDialog, input voiceInitialRequest) sipkit.IMSRequestOptions {
	securityMode := "disabled"
	if strings.TrimSpace(dialog.securityVerify) != "" {
		securityMode = "enabled"
	}
	headers := make([]sip.Header, 0, 2)
	if strings.TrimSpace(dialog.contactHeader) != "" {
		headers = append(headers, sip.NewHeader("Contact", strings.TrimSpace(dialog.contactHeader)))
	}
	if strings.TrimSpace(dialog.sessionID) != "" {
		headers = append(headers, sip.NewHeader("Session-ID", strings.TrimSpace(dialog.sessionID)))
	}
	contentType := ""
	if input.sdp != "" {
		contentType = "application/sdp"
	}
	return sipkit.IMSRequestOptions{
		Transport: dialog.transport, Branch: dialog.inviteBranch, FromURI: input.from, FromTag: dialog.localTag,
		ToURI: input.to, CallID: input.callID, CSeq: uint32(dialog.inviteCSeq), Routes: dialog.serviceRoute,
		Body: []byte(input.sdp), Kind: sipkit.RequestKindOutOfDialog, SecurityMode: securityMode,
		AddRPort: true, OmitURITransport: true, AddPreferredService: true,
		PreferredService: "urn:urn-7:3gpp-service.ims.icsi.mmtel", AddAcceptContact: true,
		AcceptContact: `*;+g.3gpp.icsi-ref="urn%3Aurn-7%3A3gpp-service.ims.icsi.mmtel"`,
		AddUserAgent:  strings.TrimSpace(dialog.userAgent) != "", UserAgent: dialog.userAgent,
		AddSupported: true, Supported: voiceInviteSupported, AddAllow: true, Allow: voiceInviteAllow,
		PreferredIdentity: "<" + dialog.localURI + ">", SecurityVerify: dialog.securityVerify,
		Runtime:     sipkit.IMSRuntimeSnapshot{PAccessNetworkInfo: dialog.pani, LocalAddr: dialog.localAddress, Transport: dialog.transport},
		ContentType: contentType, Headers: headers,
	}
}

func parseVoiceURI(value string) (sip.Uri, error) {
	var uri sip.Uri
	if err := sip.ParseUri(strings.TrimSpace(value), &uri); err != nil {
		return sip.Uri{}, err
	}
	return uri, nil
}

// BuildIMSCancel builds a CANCEL matching the initial INVITE transaction.
func BuildIMSCancel(agent *Agent, call *Call) string {
	request, _ := buildIMSCancel(agent, call)
	return request
}

func buildIMSCancel(agent *Agent, call *Call) (string, error) {
	if agent == nil || call == nil {
		return "", errors.New("voice: missing agent or call")
	}
	invite := call.outboundInviteSnapshot()
	if strings.TrimSpace(invite) == "" {
		dialog := ensureBuilderVoiceDialog(agent, call)
		dialog.cseq = dialog.inviteCSeq
		invite = buildVoiceRequest(dialog, call.CallID(), "INVITE", dialog.inviteBranch, "")
	}
	message, err := sip.ParseMessage([]byte(invite))
	if err != nil {
		return "", fmt.Errorf("voice: parse initial INVITE: %w", err)
	}
	inviteRequest, ok := message.(*sip.Request)
	if !ok {
		return "", errors.New("voice: initial INVITE is not a request")
	}
	cancel, err := sipkit.BuildCancelFromInvite(inviteRequest)
	if err != nil {
		return "", fmt.Errorf("voice: build CANCEL from INVITE: %w", err)
	}
	return cancel.String(), nil
}
