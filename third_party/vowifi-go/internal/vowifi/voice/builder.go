package voice

import (
	"crypto/rand"
	"fmt"
	"net"
	"strings"
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
	if agent == nil || call == nil {
		return ""
	}
	dialog := call.voiceDialogSnapshot()
	if dialog.remoteURI == "" {
		dialog = fallbackVoiceDialog(agent, call)
		call.setVoiceDialog(&dialog)
	}
	if strings.TrimSpace(sdp) == "" {
		sdp = generateBasicSDP(agent, call)
	}
	var request strings.Builder
	fmt.Fprintf(&request, "INVITE %s SIP/2.0\r\n", dialog.remoteURI)
	writeVoiceCoreHeaders(&request, dialog, call.CallID(), "INVITE", dialog.inviteBranch)
	writeVoiceOptionalHeader(&request, "Contact", dialog.contactHeader)
	if dialog.securityVerify != "" {
		request.WriteString("Require: sec-agree\r\nProxy-Require: sec-agree\r\n")
	}
	request.WriteString("Supported: " + voiceInviteSupported + "\r\n")
	request.WriteString("Allow: " + voiceInviteAllow + "\r\n")
	request.WriteString("Accept-Contact: *;+g.3gpp.icsi-ref=\"urn%3Aurn-7%3A3gpp-service.ims.icsi.mmtel\"\r\n")
	request.WriteString("P-Preferred-Service: urn:urn-7:3gpp-service.ims.icsi.mmtel\r\n")
	writeVoiceOptionalHeader(&request, "P-Access-Network-Info", dialog.pani)
	writeVoiceOptionalHeader(&request, "P-Preferred-Identity", "<"+dialog.localURI+">")
	writeVoiceOptionalHeader(&request, "Security-Verify", dialog.securityVerify)
	writeVoiceOptionalHeader(&request, "User-Agent", dialog.userAgent)
	writeVoiceOptionalHeader(&request, "Session-ID", dialog.sessionID)
	if sdp != "" {
		request.WriteString("Content-Type: application/sdp\r\n")
	}
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

func buildIMSReinvite(agent *Agent, call *Call) string {
	if agent == nil || call == nil {
		return ""
	}
	dialog := call.advanceVoiceInviteCSeq()
	sdp := call.imsLocalSDPValue()
	if strings.TrimSpace(sdp) == "" {
		sdp = generateBasicSDP(agent, call)
	}
	return buildVoiceRequest(dialog, call.CallID(), "INVITE", voiceBranch(), sdp)
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
	dialog.cseq = dialog.inviteCSeq
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
	dialog.cseq = dialog.inviteCSeq
	return buildVoiceRequest(dialog, call.CallID(), "CANCEL", dialog.inviteBranch, "")
}

func buildIMSPrack(agent *Agent, call *Call, rseq uint32) string {
	if agent == nil || call == nil || rseq == 0 {
		return ""
	}
	dialog := call.advanceVoiceCSeq()
	request := buildVoiceRequest(dialog, call.CallID(), "PRACK", voiceBranch(), "")
	rack := fmt.Sprintf("RAck: %d %d INVITE\r\n", rseq, dialog.inviteCSeq)
	return strings.Replace(request, "Content-Length: 0\r\n", rack+"Content-Length: 0\r\n", 1)
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
	writeVoiceCoreHeaders(out, dialog, callID, method, branch)
	writeVoiceOptionalHeader(out, "P-Preferred-Identity", "<"+dialog.localURI+">")
	writeVoiceOptionalHeader(out, "Security-Verify", dialog.securityVerify)
	writeVoiceOptionalHeader(out, "P-Access-Network-Info", dialog.pani)
	writeVoiceOptionalHeader(out, "User-Agent", dialog.userAgent)
}

func writeVoiceCoreHeaders(out *strings.Builder, dialog voiceSIPDialog, callID, method, branch string) {
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
	remoteURI := buildIMSCalledPartyURI(call.Peer(), localURI, domain)
	return voiceSIPDialog{
		localURI: localURI, remoteURI: remoteURI, remoteTarget: remoteURI,
		contactURI: localURI, contactHeader: "<" + localURI + ">",
		localAddress: agent.localAddr(), transport: "udp",
		serviceRoute: agent.ims.GetServiceRoute(), securityVerify: agent.ims.GetSecurityVerify(),
		pani: agent.ims.GetPAccessNetworkInfo(), localTag: voiceTag(), inviteBranch: voiceBranch(),
		sessionID: voiceSessionID(), cseq: 1,
		inviteCSeq: 1,
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
	if port <= 0 {
		return ""
	}
	ipFamily := sdpIPFamily(ip)
	sessionID := voiceSessID()
	return fmt.Sprintf("v=0\r\no=- %d %d IN %s %s\r\ns=VoHive Call\r\nc=IN %s %s\r\nt=0 0\r\nm=audio %d RTP/AVP 104 114 9 8 0 101\r\nb=AS:50\r\na=rtpmap:104 AMR-WB/16000\r\na=fmtp:104 octet-align=1; max-red=0\r\na=rtpmap:114 AMR/8000\r\na=fmtp:114 octet-align=1; max-red=0\r\na=rtpmap:9 G722/8000\r\na=rtpmap:8 PCMA/8000\r\na=rtpmap:0 PCMU/8000\r\na=rtpmap:101 telephone-event/8000\r\na=fmtp:101 0-15\r\na=sendrecv\r\na=ptime:20\r\na=maxptime:20\r\n", sessionID, sessionID, ipFamily, ip, ipFamily, ip, port)
}

func (a *Agent) localAddr() string {
	if a == nil || a.ims == nil || a.ims.GetLocalIMSAddr() == "" {
		return "0.0.0.0:5060"
	}
	return a.ims.GetLocalIMSAddr()
}

func (a *Agent) localIP() string {
	return voiceHost(a.localAddr())
}

func voiceHost(address string) string {
	address = strings.TrimSpace(address)
	if ip := net.ParseIP(strings.Trim(address, "[]")); ip != nil {
		return ip.String()
	}
	host, _, err := net.SplitHostPort(address)
	if err == nil {
		return strings.Trim(host, "[]")
	}
	return strings.Trim(address, "[]")
}

func sdpIPFamily(address string) string {
	ip := net.ParseIP(strings.TrimSpace(address))
	if ip != nil && ip.To4() == nil {
		return "IP6"
	}
	return "IP4"
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

func buildIMSCalledPartyURI(phone, publicIdentity, fallbackDomain string) string {
	digits := sanitizeVoicePhone(phone)
	if digits == "" {
		return ""
	}
	user := digits
	normalized := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(phone), "tel:"))
	if strings.HasPrefix(normalized, "+") {
		user = "+" + digits
	}
	domain := publicIdentityDomain(publicIdentity)
	if domain == "" {
		domain = strings.TrimSpace(fallbackDomain)
	}
	if domain == "" {
		return ""
	}
	return "sip:" + user + "@" + domain + ";user=phone"
}

func publicIdentityDomain(identity string) string {
	identity = strings.Trim(strings.TrimSpace(identity), "<>")
	at := strings.LastIndexByte(identity, '@')
	if at < 0 || at == len(identity)-1 {
		return ""
	}
	domain, _, _ := strings.Cut(identity[at+1:], ";")
	return strings.TrimSpace(domain)
}

func voiceTag() string       { return voiceHex(16) }
func voiceBranch() string    { return "z9hG4bK-" + voiceHex(24) }
func voiceSessionID() string { return voiceHex(32) }

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
