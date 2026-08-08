package voice

import (
	"errors"
	"strings"

	"github.com/iniwex5/vowifi-go/internal/vowifi/imscore"
)

type voiceSIPDialog struct {
	localURI       string
	remoteURI      string
	remoteTarget   string
	contactURI     string
	contactHeader  string
	localAddress   string
	transport      string
	serviceRoute   []string
	securityVerify string
	pani           string
	userAgent      string
	localTag       string
	remoteTag      string
	inviteBranch   string
	sessionID      string
	cseq           int
	inviteCSeq     int
}

func (a *Agent) prepareVoiceDialog(call *Call, number string) error {
	if a == nil || a.ims == nil || call == nil {
		return errors.New("voice: IMS service is unavailable")
	}
	profile, err := a.ims.RegisteredSIPDialogProfile()
	if err != nil {
		return err
	}
	remoteURI := buildIMSCalledPartyURI(number, profile.LocalURI, profile.Domain)
	if remoteURI == "" {
		return errors.New("voice: callee is empty")
	}
	initialCSeq := profile.InitialCSeq
	if initialCSeq <= 0 {
		initialCSeq = 1
	}
	call.setVoiceDialog(&voiceSIPDialog{
		localURI: profile.LocalURI, remoteURI: remoteURI, remoteTarget: remoteURI,
		contactURI: profile.ContactURI, contactHeader: profile.ContactHeader,
		localAddress: profile.LocalAddress,
		transport:    profile.Transport, serviceRoute: splitVoiceHeaderValues(profile.ServiceRoute),
		securityVerify: profile.SecurityVerify, pani: profile.PANI, userAgent: profile.UserAgent,
		localTag: voiceTag(), inviteBranch: voiceBranch(), sessionID: voiceSessionID(),
		cseq: initialCSeq, inviteCSeq: initialCSeq,
	})
	return nil
}

func (c *Call) setVoiceDialog(dialog *voiceSIPDialog) {
	c.mu.Lock()
	c.sipDialog = dialog
	c.mu.Unlock()
}

func (c *Call) voiceDialogSnapshot() voiceSIPDialog {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.sipDialog == nil {
		return voiceSIPDialog{}
	}
	copy := *c.sipDialog
	copy.serviceRoute = append([]string(nil), c.sipDialog.serviceRoute...)
	return copy
}

func (c *Call) advanceVoiceCSeq() voiceSIPDialog {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.sipDialog == nil {
		return voiceSIPDialog{}
	}
	c.sipDialog.cseq++
	copy := *c.sipDialog
	copy.serviceRoute = append([]string(nil), c.sipDialog.serviceRoute...)
	return copy
}

func (c *Call) advanceVoiceInviteCSeq() voiceSIPDialog {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.sipDialog == nil {
		return voiceSIPDialog{}
	}
	c.sipDialog.cseq++
	c.sipDialog.inviteCSeq = c.sipDialog.cseq
	copy := *c.sipDialog
	copy.serviceRoute = append([]string(nil), c.sipDialog.serviceRoute...)
	return copy
}

func (c *Call) learnVoiceDialog(response imscore.SIPResponse) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.sipDialog == nil {
		return
	}
	c.sipDialog.remoteTag = voiceHeaderTag(voiceResponseHeader(response.Headers, "To"))
	if contact := voiceHeaderURI(voiceResponseHeader(response.Headers, "Contact")); contact != "" {
		c.sipDialog.remoteTarget = contact
	}
	routes := splitVoiceHeaderValues(voiceResponseHeader(response.Headers, "Record-Route"))
	for left, right := 0, len(routes)-1; left < right; left, right = left+1, right-1 {
		routes[left], routes[right] = routes[right], routes[left]
	}
	if len(routes) > 0 {
		c.sipDialog.serviceRoute = routes
	}
}

func voiceResponseHeader(headers map[string]string, name string) string {
	for key, value := range headers {
		if strings.EqualFold(key, name) {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func voiceHeaderTag(value string) string {
	for _, parameter := range strings.Split(value, ";")[1:] {
		name, tag, ok := strings.Cut(strings.TrimSpace(parameter), "=")
		if ok && strings.EqualFold(name, "tag") {
			return strings.Trim(strings.TrimSpace(tag), "\"")
		}
	}
	return ""
}

func voiceHeaderURI(value string) string {
	start, end := strings.IndexByte(value, '<'), strings.IndexByte(value, '>')
	if start >= 0 && end > start {
		return strings.TrimSpace(value[start+1 : end])
	}
	return strings.TrimSpace(strings.Split(value, ";")[0])
}

func splitVoiceHeaderValues(value string) []string {
	var values []string
	for _, item := range strings.Split(value, ",") {
		if item = strings.TrimSpace(item); item != "" {
			values = append(values, item)
		}
	}
	return values
}
