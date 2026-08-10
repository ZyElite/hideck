package voice

import (
	"strings"

	"github.com/emiago/sipgo/sip"
)

// BuildResponse retains the recovered structured SIP response API.
func (c *Call) BuildResponse(status int, reason string) *sip.Response {
	return c.buildResponseMessage(status, reason, nil)
}

// BuildResponseWithSDP retains the recovered structured SDP response API.
func (c *Call) BuildResponseWithSDP(status int, reason string, sdp []byte) *sip.Response {
	return c.buildResponseMessage(status, reason, append([]byte(nil), sdp...))
}

func (c *Call) buildResponseMessage(status int, reason string, body []byte) *sip.Response {
	if c == nil {
		return nil
	}
	c.mu.RLock()
	request := c.DialogState.OriginalRequest
	c.mu.RUnlock()
	var response *sip.Response
	if request == nil {
		response = sip.NewResponse(status, reason)
		response.SetBody(body)
	} else {
		response = sip.NewResponseFromRequest(request.Clone(), status, reason, body)
	}
	c.applyStableResponseToTag(response, status)
	if status >= 200 && status < 300 {
		response.AppendHeader(sip.NewHeader("Supported", "timer"))
	}
	c.appendSessionContact(response, status)
	if len(body) > 0 {
		response.RemoveHeader("Content-Type")
		response.AppendHeader(sip.NewHeader("Content-Type", "application/sdp"))
		response.SetBody(body)
	}
	return response
}

func (c *Call) appendSessionContact(response *sip.Response, status int) {
	if c == nil || response == nil || status <= 100 {
		return
	}
	c.mu.RLock()
	session := c.DialogState.IMSSession
	calleeID := c.DialogState.CalleeID
	c.mu.RUnlock()
	if session == nil || strings.TrimSpace(session.LocalIP) == "" {
		return
	}
	response.AppendHeader(&sip.ContactHeader{Address: sip.Uri{
		Scheme: "sip", User: calleeID, Host: session.LocalIP, Port: session.LocalPortC,
	}})
}

func (c *Call) applyStableResponseToTag(response *sip.Response, status int) {
	if c == nil || response == nil || status <= 100 || response.To() == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if strings.TrimSpace(c.DialogState.ToTag) == "" {
		c.DialogState.ToTag = sipHeaderTag(response.To())
		return
	}
	response.To().Params.Add("tag", c.DialogState.ToTag)
}
