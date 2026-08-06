package voice

import (
	"crypto/rand"
	"fmt"
	"strings"
)

// BuildIMSInvite builds an outbound INVITE request (RFC 3261) with an SDP
// offer for the given call.
func BuildIMSInvite(a *Agent, c *Call) string {
	if a == nil || c == nil {
		return ""
	}
	cfg := a.imsConfig()
	domain := cfg.Domain
	if domain == "" {
		domain = "ims.mnc000.mcc000.3gppnetwork.org"
	}
	impi := cfg.IMPI
	if impi == "" {
		impi = a.deviceID
	}
	callID := c.CallID()
	peer := sanitizeVoicePhone(c.Peer())
	fromTag := voiceTag()
	branch := voiceBranch()
	sdp := generateBasicSDP(a, c)

	var b strings.Builder
	b.WriteString(fmt.Sprintf("INVITE sip:%s@%s SIP/2.0\r\n", peer, domain))
	b.WriteString(fmt.Sprintf("Via: SIP/2.0/UDP %s;branch=%s;rport\r\n", a.localAddr(), branch))
	b.WriteString(fmt.Sprintf("From: <sip:%s@%s>;tag=%s\r\n", impi, domain, fromTag))
	b.WriteString(fmt.Sprintf("To: <sip:%s@%s>\r\n", peer, domain))
	b.WriteString(fmt.Sprintf("Call-ID: %s\r\n", callID))
	b.WriteString("CSeq: 1 INVITE\r\n")
	b.WriteString("Max-Forwards: 70\r\n")
	b.WriteString("Content-Type: application/sdp\r\n")
	b.WriteString("Supported: 100rel, precondition\r\n")
	b.WriteString(fmt.Sprintf("Content-Length: %d\r\n\r\n", len(sdp)))
	b.WriteString(sdp)
	return b.String()
}

// BuildIMSBye builds a BYE request for a dialog.
func BuildIMSBye(a *Agent, c *Call) string {
	if a == nil || c == nil {
		return ""
	}
	cfg := a.imsConfig()
	domain := cfg.Domain
	if domain == "" {
		domain = "ims.mnc000.mcc000.3gppnetwork.org"
	}
	impi := cfg.IMPI
	if impi == "" {
		impi = a.deviceID
	}
	peer := sanitizeVoicePhone(c.Peer())
	toTag := ""
	if d := c.IMSDialog(); d != nil {
		toTag = d.ToTag()
	}
	var b strings.Builder
	b.WriteString(fmt.Sprintf("BYE sip:%s@%s SIP/2.0\r\n", peer, domain))
	b.WriteString(fmt.Sprintf("Via: SIP/2.0/UDP %s;branch=%s;rport\r\n", a.localAddr(), voiceBranch()))
	b.WriteString(fmt.Sprintf("From: <sip:%s@%s>;tag=%s\r\n", impi, domain, voiceTag()))
	b.WriteString(fmt.Sprintf("To: <sip:%s@%s>;tag=%s\r\n", peer, domain, toTag))
	b.WriteString(fmt.Sprintf("Call-ID: %s\r\n", c.CallID()))
	b.WriteString("CSeq: 2 BYE\r\n")
	b.WriteString("Max-Forwards: 70\r\n")
	b.WriteString("Content-Length: 0\r\n\r\n")
	return b.String()
}

// BuildIMSACK builds an ACK for a 2xx INVITE response.
func BuildIMSACK(a *Agent, c *Call) string {
	if a == nil || c == nil {
		return ""
	}
	cfg := a.imsConfig()
	domain := cfg.Domain
	if domain == "" {
		domain = "ims.mnc000.mcc000.3gppnetwork.org"
	}
	impi := cfg.IMPI
	if impi == "" {
		impi = a.deviceID
	}
	peer := sanitizeVoicePhone(c.Peer())
	var b strings.Builder
	b.WriteString(fmt.Sprintf("ACK sip:%s@%s SIP/2.0\r\n", peer, domain))
	b.WriteString(fmt.Sprintf("Via: SIP/2.0/UDP %s;branch=%s;rport\r\n", a.localAddr(), voiceBranch()))
	b.WriteString(fmt.Sprintf("From: <sip:%s@%s>;tag=%s\r\n", impi, domain, voiceTag()))
	b.WriteString(fmt.Sprintf("To: <sip:%s@%s>\r\n", peer, domain))
	b.WriteString(fmt.Sprintf("Call-ID: %s\r\n", c.CallID()))
	b.WriteString("CSeq: 1 ACK\r\n")
	b.WriteString("Max-Forwards: 70\r\n")
	b.WriteString("Content-Length: 0\r\n\r\n")
	return b.String()
}

// BuildIMSCancel builds a CANCEL for an outstanding INVITE.
func BuildIMSCancel(a *Agent, c *Call) string {
	if a == nil || c == nil {
		return ""
	}
	cfg := a.imsConfig()
	domain := cfg.Domain
	if domain == "" {
		domain = "ims.mnc000.mcc000.3gppnetwork.org"
	}
	impi := cfg.IMPI
	if impi == "" {
		impi = a.deviceID
	}
	peer := sanitizeVoicePhone(c.Peer())
	var b strings.Builder
	b.WriteString(fmt.Sprintf("CANCEL sip:%s@%s SIP/2.0\r\n", peer, domain))
	b.WriteString(fmt.Sprintf("Via: SIP/2.0/UDP %s;branch=%s;rport\r\n", a.localAddr(), voiceBranch()))
	b.WriteString(fmt.Sprintf("From: <sip:%s@%s>;tag=%s\r\n", impi, domain, voiceTag()))
	b.WriteString(fmt.Sprintf("To: <sip:%s@%s>\r\n", peer, domain))
	b.WriteString(fmt.Sprintf("Call-ID: %s\r\n", c.CallID()))
	b.WriteString("CSeq: 1 CANCEL\r\n")
	b.WriteString("Max-Forwards: 70\r\n")
	b.WriteString("Content-Length: 0\r\n\r\n")
	return b.String()
}

// generateBasicSDP builds a minimal audio SDP offer (RFC 4566).
func generateBasicSDP(a *Agent, c *Call) string {
	ip := a.localIP()
	if ip == "" {
		ip = "0.0.0.0"
	}
	port := a.mediaPort()
	var b strings.Builder
	b.WriteString("v=0\r\n")
	b.WriteString(fmt.Sprintf("o=- %d %d IN IP4 %s\r\n", voiceSessID(), voiceSessID(), ip))
	b.WriteString("s=VoWiFi call\r\n")
	b.WriteString("c=IN IP4 " + ip + "\r\n")
	b.WriteString("t=0 0\r\n")
	b.WriteString(fmt.Sprintf("m=audio %d RTP/AVP 96 97 98\r\n", port))
	b.WriteString("a=rtpmap:96 AMR-WB/16000/1\r\n")
	b.WriteString("a=rtpmap:97 AMR/8000/1\r\n")
	b.WriteString("a=rtpmap:98 telephone-event/8000\r\n")
	b.WriteString("a=fmtp:96 mode-set=0,1,2,3,4,5,6,7\r\n")
	b.WriteString("a=sendrecv\r\n")
	return b.String()
}

// imsConfig returns the IMS config for the agent's service.
func (a *Agent) imsConfig() *imsConfigView {
	if a == nil || a.ims == nil {
		return &imsConfigView{}
	}
	return &imsConfigView{
		Domain: a.ims.GetRealm(),
		IMPI:   a.ims.GetIMSI(),
	}
}

// imsConfigView is a minimal view of the IMS config used by builders.
type imsConfigView struct {
	Domain string
	IMPI   string
}

// localAddr returns the local SIP address for Via headers.
func (a *Agent) localAddr() string {
	if a == nil || a.ims == nil {
		return "0.0.0.0:5060"
	}
	addr := a.ims.GetLocalIMSAddr()
	if addr == "" {
		return "0.0.0.0:5060"
	}
	return addr
}

// localIP returns the local IP for SDP.
func (a *Agent) localIP() string {
	addr := a.localAddr()
	if i := strings.IndexByte(addr, ':'); i > 0 {
		return addr[:i]
	}
	return addr
}

// mediaPort returns the local media port.
func (a *Agent) mediaPort() int {
	if a == nil {
		return 0
	}
	a.mu.RLock()
	call := a.activeCall
	a.mu.RUnlock()
	if call != nil {
		if r := call.RTPRelay(); r != nil {
			if p := r.IMSPort(); p != 0 {
				return p
			}
		}
	}
	return 0
}

// sanitizeVoicePhone strips non-digit characters from a phone number.
func sanitizeVoicePhone(p string) string {
	var b strings.Builder
	for _, c := range p {
		if c >= '0' && c <= '9' {
			b.WriteRune(c)
		}
	}
	return b.String()
}

// voiceTag generates a From tag.
func voiceTag() string {
	return voiceHex(8)
}

// voiceBranch generates a Via branch.
func voiceBranch() string {
	return "z9hG4bK" + voiceHex(16)
}

// voiceSessID generates an SDP session ID.
func voiceSessID() int64 {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	var n int64
	for _, x := range b {
		n = n*256 + int64(x)
	}
	if n < 0 {
		n = -n
	}
	return n % 1000000000
}

// voiceHex generates a hex string of n random bytes.
func voiceHex(n int) string {
	const digits = "0123456789abcdef"
	b := make([]byte, n)
	_, _ = randVoiceRead(b)
	for i := range b {
		b[i] = digits[int(b[i])%16]
	}
	return string(b)
}

// randVoiceRead fills b with random bytes.
func randVoiceRead(b []byte) (int, error) {
	return rand.Read(b)
}
