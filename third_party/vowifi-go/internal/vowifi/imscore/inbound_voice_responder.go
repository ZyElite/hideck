package imscore

import (
	"errors"
	"fmt"
	"strings"
	"sync"
)

type networkInboundVoiceResponder struct {
	mu       sync.Mutex
	request  string
	reply    func(string) error
	localTag string
	final    bool
}

func newInboundVoiceResponder(request string, reply func(string) error) InboundVoiceResponder {
	if reply == nil {
		return nil
	}
	return &networkInboundVoiceResponder{request: request, reply: reply, localTag: newTag()}
}

func (r *networkInboundVoiceResponder) LocalTag() string {
	if r == nil {
		return ""
	}
	return r.localTag
}

func (r *networkInboundVoiceResponder) Respond(response InboundVoiceResponse) error {
	if r == nil || r.reply == nil {
		return errors.New("imscore: inbound voice reply path is unavailable")
	}
	if response.StatusCode < 100 || response.StatusCode > 699 {
		return fmt.Errorf("imscore: invalid SIP response status %d", response.StatusCode)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.final {
		return errors.New("imscore: inbound voice final response already sent")
	}
	localTag := r.localTag
	if strings.TrimSpace(response.ToTag) != "" {
		localTag = strings.TrimSpace(response.ToTag)
	}
	wire, err := buildSIPVoiceResponse(r.request, localTag, response)
	if err != nil {
		return err
	}
	if err := r.reply(wire); err != nil {
		return err
	}
	if response.StatusCode >= 200 {
		r.final = true
	}
	return nil
}

func buildSIPVoiceResponse(request, localTag string, response InboundVoiceResponse) (string, error) {
	base, err := inboundResponseHeaders(request, localTag)
	if err != nil {
		return "", err
	}
	var out strings.Builder
	fmt.Fprintf(&out, "SIP/2.0 %d %s\r\n%s", response.StatusCode, SIPStatusText(response.StatusCode), base)
	if value := strings.TrimSpace(response.Contact); value != "" {
		if strings.ContainsAny(value, "\r\n") {
			return "", errors.New("imscore: invalid SIP Contact response header")
		}
		fmt.Fprintf(&out, "Contact: <%s>\r\n", strings.Trim(value, "<>"))
	}
	if len(response.Body) > 0 {
		contentType := strings.TrimSpace(response.ContentType)
		if contentType == "" || strings.ContainsAny(contentType, "\r\n") {
			return "", errors.New("imscore: valid Content-Type is required for SIP response body")
		}
		fmt.Fprintf(&out, "Content-Type: %s\r\n", contentType)
	}
	fmt.Fprintf(&out, "Content-Length: %d\r\n\r\n", len(response.Body))
	out.Write(response.Body)
	return out.String(), nil
}

func inboundResponseHeaders(request, localTag string) (string, error) {
	if sipRequestMethod(request) == "" {
		return "", errors.New("imscore: invalid inbound SIP request line")
	}
	required := []string{"Via", "From", "To", "Call-ID", "CSeq"}
	values := make(map[string]string, len(required))
	for _, name := range required {
		values[name] = rawSIPHeaderValue(request, name)
		if values[name] == "" {
			return "", fmt.Errorf("imscore: inbound SIP request missing %s", name)
		}
	}
	to := values["To"]
	if !strings.Contains(strings.ToLower(to), ";tag=") {
		to += ";tag=" + localTag
	}
	return fmt.Sprintf("Via: %s\r\nFrom: %s\r\nTo: %s\r\nCall-ID: %s\r\nCSeq: %s\r\n",
		values["Via"], values["From"], to, values["Call-ID"], values["CSeq"]), nil
}
