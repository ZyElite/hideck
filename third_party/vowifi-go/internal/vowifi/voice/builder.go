package voice

import (
	"crypto/rand"
	"fmt"
	"strings"
)

// BuildIMSInvite builds the initial IMS INVITE with the registered route.
func BuildIMSInvite(agent *Agent, call *Call) string {
	if agent == nil || call == nil {
		return ""
	}
	dialog := call.voiceDialogSnapshot()
	if dialog.remoteURI == "" {
		dialog = fallbackVoiceDialog(agent, call)
		call.setVoiceDialog(&dialog)
	}
	sdp := generateBasicSDP(agent, call)
	var request strings.Builder
	fmt.Fprintf(&request, "INVITE %s SIP/2.0\r\n", dialog.remoteURI)
	writeVoiceDialogHeaders(&request, dialog, call.CallID(), "INVITE", dialog.inviteBranch)
	writeVoiceOptionalHeader(&request, "Contact", "<"+dialog.contactURI+">")
	request.WriteString("P-Preferred-Service: urn:urn-7:3gpp-service.ims.icsi.mmtel\r\n")
	request.WriteString("Accept-Contact: *;+g.3gpp.icsi-ref=\"urn%3Aurn-7%3A3gpp-service.ims.icsi.mmtel\"\r\n")
	request.WriteString("Supported: 100rel, timer\r\n")
	request.WriteString("Content-Type: application/sdp\r\n")
	fmt.Fprintf(&request, "Content-Length: %d\r\n\r\n%s", len(sdp), sdp)
	return request.String()
}

// BuildIMSBye builds an in-dialog BYE.
func BuildIMSBye(agent *Agent, call *Call) string {
	if agent == nil || call == nil {
		return ""
	}
	ensureBuilderVoiceDialog(agent, call)
	dialog := call.advanceVoiceCSeq()
	return buildVoiceRequest(dialog, call.CallID(), "BYE", voiceBranch(), "")
}

// BuildIMSACK builds the ACK for the final INVITE response.
func BuildIMSACK(agent *Agent, call *Call) string {
	return buildIMSACKForStatus(agent, call, 200)
}

func buildIMSACKForStatus(agent *Agent, call *Call, statusCode int) string {
	if agent == nil || call == nil {
		return ""
	}
	dialog := ensureBuilderVoiceDialog(agent, call)
	branch := voiceBranch()
	if statusCode >= 300 {
		branch = dialog.inviteBranch
	}
	return buildVoiceRequest(dialog, call.CallID(), "ACK", branch, "")
}

// BuildIMSCancel builds a CANCEL matching the initial INVITE transaction.
func BuildIMSCancel(agent *Agent, call *Call) string {
	if agent == nil || call == nil {
		return ""
	}
	dialog := ensureBuilderVoiceDialog(agent, call)
	return buildVoiceRequest(dialog, call.CallID(), "CANCEL", dialog.inviteBranch, "")
}

func ensureBuilderVoiceDialog(agent *Agent, call *Call) voiceSIPDialog {
	dialog := call.voiceDialogSnapshot()
	if dialog.remoteURI != "" {
		return dialog
	}
	dialog = fallbackVoiceDialog(agent, call)
	call.setVoiceDialog(&dialog)
	return dialog
}

func buildVoiceRequest(dialog voiceSIPDialog, callID, method, branch, body string) string {
	target := dialog.remoteTarget
	if target == "" {
		target = dialog.remoteURI
	}
	var request strings.Builder
	fmt.Fprintf(&request, "%s %s SIP/2.0\r\n", method, target)
	writeVoiceDialogHeaders(&request, dialog, callID, method, branch)
	if body != "" {
		request.WriteString("Content-Type: application/sdp\r\n")
	}
	fmt.Fprintf(&request, "Content-Length: %d\r\n\r\n%s", len(body), body)
	return request.String()
}

func writeVoiceDialogHeaders(out *strings.Builder, dialog voiceSIPDialog, callID, method, branch string) {
	transport := strings.ToUpper(strings.TrimSpace(dialog.transport))
	if transport == "" {
		transport = "UDP"
	}
	fmt.Fprintf(out, "Via: SIP/2.0/%s %s;rport;branch=%s\r\n", transport, dialog.localAddress, branch)
	for _, route := range dialog.serviceRoute {
		fmt.Fprintf(out, "Route: %s\r\n", route)
	}
	fmt.Fprintf(out, "From: <%s>;tag=%s\r\n", dialog.localURI, dialog.localTag)
	to := "<" + dialog.remoteURI + ">"
	if dialog.remoteTag != "" {
		to += ";tag=" + dialog.remoteTag
	}
	fmt.Fprintf(out, "To: %s\r\nCall-ID: %s\r\nCSeq: %d %s\r\n", to, callID, dialog.cseq, method)
	out.WriteString("Max-Forwards: 70\r\n")
	writeVoiceOptionalHeader(out, "P-Preferred-Identity", "<"+dialog.localURI+">")
	writeVoiceOptionalHeader(out, "Security-Verify", dialog.securityVerify)
	writeVoiceOptionalHeader(out, "P-Access-Network-Info", dialog.pani)
	writeVoiceOptionalHeader(out, "User-Agent", dialog.userAgent)
}

func writeVoiceOptionalHeader(out *strings.Builder, name, value string) {
	if strings.TrimSpace(value) != "" && value != "<>" {
		fmt.Fprintf(out, "%s: %s\r\n", name, strings.TrimSpace(value))
	}
}

func fallbackVoiceDialog(agent *Agent, call *Call) voiceSIPDialog {
	domain := agent.ims.GetRealm()
	localURI := "sip:" + agent.ims.GetIMSI() + "@" + domain
	if identities := agent.ims.GetIMPU(); len(identities) > 0 && strings.TrimSpace(identities[0]) != "" {
		localURI = strings.TrimSpace(identities[0])
	}
	remoteURI := "sip:" + sanitizeVoicePhone(call.Peer()) + "@" + domain
	return voiceSIPDialog{
		localURI: localURI, remoteURI: remoteURI, remoteTarget: remoteURI,
		contactURI: localURI, localAddress: agent.localAddr(), transport: "udp",
		serviceRoute: agent.ims.GetServiceRoute(), securityVerify: agent.ims.GetSecurityVerify(),
		pani: agent.ims.GetPAccessNetworkInfo(), localTag: voiceTag(), inviteBranch: voiceBranch(), cseq: 1,
	}
}

type imsConfigView struct {
	Domain string
	IMPI   string
}

func (a *Agent) imsConfig() *imsConfigView {
	if a == nil || a.ims == nil {
		return &imsConfigView{}
	}
	profile := a.ims.VoiceProfile()
	return &imsConfigView{Domain: profile.Domain, IMPI: profile.IMPI}
}

func generateBasicSDP(agent *Agent, call *Call) string {
	ip := agent.localIP()
	if ip == "" {
		ip = "0.0.0.0"
	}
	port := agent.mediaPort()
	sessionID := voiceSessID()
	return fmt.Sprintf("v=0\r\no=- %d %d IN IP4 %s\r\ns=VoWiFi call\r\nc=IN IP4 %s\r\nt=0 0\r\nm=audio %d RTP/AVP 96 97 98\r\na=rtpmap:96 AMR-WB/16000/1\r\na=rtpmap:97 AMR/8000/1\r\na=rtpmap:98 telephone-event/8000\r\na=fmtp:96 mode-set=0,1,2,3,4,5,6,7\r\na=sendrecv\r\n", sessionID, sessionID, ip, ip, port)
}

func (a *Agent) localAddr() string {
	if a == nil || a.ims == nil || a.ims.GetLocalIMSAddr() == "" {
		return "0.0.0.0:5060"
	}
	return a.ims.GetLocalIMSAddr()
}

func (a *Agent) localIP() string {
	address := a.localAddr()
	if index := strings.LastIndexByte(address, ':'); index > 0 {
		return strings.Trim(address[:index], "[]")
	}
	return strings.Trim(address, "[]")
}

func (a *Agent) mediaPort() int {
	if a == nil {
		return 0
	}
	a.mu.RLock()
	call := a.activeCall
	a.mu.RUnlock()
	if call != nil && call.RTPRelay() != nil {
		return call.RTPRelay().IMSPort()
	}
	return 0
}

func sanitizeVoicePhone(phone string) string {
	var sanitized strings.Builder
	for _, char := range phone {
		if char >= '0' && char <= '9' {
			sanitized.WriteRune(char)
		}
	}
	return sanitized.String()
}

func voiceTag() string    { return voiceHex(8) }
func voiceBranch() string { return "z9hG4bK" + voiceHex(16) }

func voiceSessID() int64 {
	bytes := make([]byte, 8)
	_, _ = rand.Read(bytes)
	var value int64
	for _, item := range bytes {
		value = value*256 + int64(item)
	}
	if value < 0 {
		value = -value
	}
	return value % 1000000000
}

func voiceHex(length int) string {
	const digits = "0123456789abcdef"
	bytes := make([]byte, length)
	_, _ = randVoiceRead(bytes)
	for index := range bytes {
		bytes[index] = digits[int(bytes[index])%len(digits)]
	}
	return string(bytes)
}

func randVoiceRead(bytes []byte) (int, error) { return rand.Read(bytes) }
