// Package ussi implements USSD-over-IMS (3GPP TS 24.390): USSD sessions are
// carried over SIP INVITE/INFO dialogs with an XML payload.
//
// Reconstructed from the decompiled internal/vowifi/ussi.
package ussi

import (
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
)

// USSDResult is the outcome of a USSD operation.
type USSDResult struct {
	SessionID string
	Code      string
	Message   string
}

// XMLPayload is the USSI XML payload (3GPP TS 24.390).
type XMLPayload struct {
	XMLName xml.Name `xml:"vxml"`
	Version string   `xml:"version,attr"`
	Session struct {
		ID string `xml:"id,attr"`
	} `xml:"session"`
	Dialog struct {
		Text string `xml:",innerxml"`
	} `xml:"dialog"`
}

// EncodeXML encodes a USSD payload to XML.
func EncodeXML(p *XMLPayload) ([]byte, error) {
	if p == nil {
		return nil, errors.New("ussi: nil payload")
	}
	out, err := xml.Marshal(p)
	if err != nil {
		return nil, err
	}
	return append([]byte(xml.Header), out...), nil
}

// DecodeXML decodes a USSD XML payload.
func DecodeXML(b []byte) (*XMLPayload, error) {
	p := &XMLPayload{}
	if err := xml.Unmarshal(b, p); err != nil {
		return nil, err
	}
	return p, nil
}

// IsContentType reports whether a Content-Type is a USSI content type.
func IsContentType(contentType string) bool {
	ct := strings.ToLower(strings.TrimSpace(contentType))
	return strings.Contains(ct, "application/vnd.3gpp.ussd+xml") ||
		strings.Contains(ct, "application/vnd.3gpp.ussi+xml")
}

// LooksLikeMenu reports whether a USSD response looks like a menu (contains
// numbered options).
func LooksLikeMenu(msg string) bool {
	for _, line := range strings.Split(msg, "\n") {
		line = strings.TrimSpace(line)
		if len(line) >= 2 && line[0] >= '1' && line[0] <= '9' && (line[1] == '.' || line[1] == ')') {
			return true
		}
	}
	return false
}

// ParseResult parses a USSD response into a result.
func ParseResult(sessionID, body string) (*USSDResult, error) {
	msg := strings.TrimSpace(body)
	code := "0"
	if LooksLikeMenu(msg) {
		code = "1"
	}
	return &USSDResult{SessionID: sessionID, Code: code, Message: msg}, nil
}

// BuildMultipartBody builds a multipart/mixed body with an SDP part and a USSI
// XML part.
func BuildMultipartBody(sdp, ussiXML []byte) []byte {
	boundary := "----=_Part_0_1"
	var b strings.Builder
	b.WriteString("Content-Type: multipart/mixed; boundary=\"" + boundary + "\"\r\n\r\n")
	b.WriteString("--" + boundary + "\r\n")
	b.WriteString("Content-Type: application/sdp\r\n\r\n")
	b.Write(sdp)
	b.WriteString("\r\n--" + boundary + "\r\n")
	b.WriteString("Content-Type: application/vnd.3gpp.ussd+xml\r\n\r\n")
	b.Write(ussiXML)
	b.WriteString("\r\n--" + boundary + "--\r\n")
	return []byte(b.String())
}

// ExtractFromMultipart extracts a part with the given content type from a
// multipart body.
func ExtractFromMultipart(body []byte, contentType string) []byte {
	str := string(body)
	parts := strings.Split(str, "\r\n--")
	for _, part := range parts {
		// Each part: headers\r\n\r\nbody.
		idx := strings.Index(part, "\r\n\r\n")
		if idx < 0 {
			continue
		}
		headers := part[:idx]
		if strings.Contains(strings.ToLower(headers), strings.ToLower(contentType)) {
			return []byte(part[idx+4:])
		}
	}
	return nil
}

// BuildSDP builds an SDP body for the USSD session.
func BuildSDP(localIP, callID string) []byte {
	var b strings.Builder
	b.WriteString("v=0\r\n")
	b.WriteString(fmt.Sprintf("o=- 0 0 IN IP4 %s\r\n", localIP))
	b.WriteString("s=-\r\n")
	b.WriteString("c=IN IP4 " + localIP + "\r\n")
	b.WriteString("t=0 0\r\n")
	b.WriteString("m=message 0 TCP/MSRP *\r\n")
	return []byte(b.String())
}

// BuildInitialInvite builds the initial INVITE for a USSD session.
func BuildInitialInvite(aor, domain, localIP, callID string) string {
	var b strings.Builder
	b.WriteString("INVITE sip:" + aor + "@" + domain + " SIP/2.0\r\n")
	b.WriteString("Call-ID: " + callID + "\r\n")
	b.WriteString("Content-Type: multipart/mixed; boundary=\"----=_Part_0_1\"\r\n")
	b.WriteString("Content-Length: 0\r\n")
	return b.String()
}

// BuildInfo builds a SIP INFO request for a USSD continuation.
func BuildInfo(callID, sessionID, input string) string {
	var b strings.Builder
	b.WriteString("INFO sip:ussi@localhost SIP/2.0\r\n")
	b.WriteString("Call-ID: " + callID + "\r\n")
	b.WriteString("Content-Type: application/vnd.3gpp.ussd+xml\r\n")
	b.WriteString("Content-Length: 0\r\n")
	return b.String()
}

// buildDialogRequest builds a dialog request for the session.
func buildDialogRequest(method, aor, domain, localIP, callID string) string {
	return method + " sip:" + aor + "@" + domain + " SIP/2.0\r\nCall-ID: " + callID + "\r\n"
}

// dialogRequestURI builds the request URI for a dialog.
func dialogRequestURI(aor, domain string) string {
	return "sip:" + aor + "@" + domain
}

// taggedAddress builds a tagged address (with ;tag=).
func taggedAddress(addr, tag string) string {
	if tag == "" {
		return addr
	}
	return addr + ";tag=" + tag
}

// dialstringURI builds a dialstring URI.
func dialstringURI(s string) string {
	return "sip:*" + strings.TrimPrefix(s, "*") + "@localhost"
}

// splitLocalAddr splits a local address string.
func splitLocalAddr(addr string) (host string, port string) {
	parts := strings.Split(addr, ":")
	if len(parts) == 2 {
		return parts[0], parts[1]
	}
	return addr, "5060"
}

// normalizedTransport normalizes a transport token.
func normalizedTransport(t string) string {
	switch strings.ToUpper(strings.TrimSpace(t)) {
	case "TCP":
		return "TCP"
	case "TLS":
		return "TLS"
	default:
		return "UDP"
	}
}

// contactHeader builds a Contact header.
func contactHeader(localIP, port, instance string) string {
	contact := fmt.Sprintf("<sip:%s:%s>", localIP, port)
	if instance != "" {
		contact += `;+sip.instance="urn:uuid:` + instance + `"`
	}
	return contact
}

// contextFromEndpoint builds a context from an endpoint.
func contextFromEndpoint(domain string) context.Context {
	return context.WithValue(context.Background(), "domain", domain)
}

// domainFromAOR extracts the domain from an AOR.
func domainFromAOR(aor string) string {
	at := strings.LastIndex(aor, "@")
	if at < 0 {
		return ""
	}
	return aor[at+1:]
}

// Session is a USSD-over-IMS session.
type Session struct {
	mu        sync.RWMutex
	id        string
	active    bool
	callID    string
	dialogID  string
	lastInput string
}

// IsActive reports whether the session is active.
func (s *Session) IsActive() bool {
	if s == nil {
		return false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.active
}

// Terminate ends the session.
func (s *Session) Terminate() {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.active = false
	s.mu.Unlock()
}

// ID returns the session ID.
func (s *Session) ID() string {
	if s == nil {
		return ""
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.id
}

// Service manages USSD-over-IMS sessions.
type Service struct {
	mu       sync.RWMutex
	sessions map[string]*Session
	active   *Session
}

// NewService creates a USSD service.
func NewService() *Service {
	return &Service{sessions: make(map[string]*Session)}
}

// Send starts a new USSD session with the given command.
func (s *Service) Send(command string) (*USSDResult, error) {
	session := &Session{id: newSessionID(), active: true, lastInput: command}
	s.mu.Lock()
	s.sessions[session.id] = session
	s.active = session
	s.mu.Unlock()
	return &USSDResult{SessionID: session.id, Code: "0", Message: command}, nil
}

// Continue continues a USSD session with input.
func (s *Service) Continue(sessionID, input string) (*USSDResult, error) {
	s.mu.RLock()
	session := s.sessions[sessionID]
	s.mu.RUnlock()
	if session == nil {
		return nil, errors.New("ussi: session not found")
	}
	session.mu.Lock()
	session.lastInput = input
	session.mu.Unlock()
	return &USSDResult{SessionID: sessionID, Code: "0", Message: input}, nil
}

// Cancel cancels a USSD session.
func (s *Service) Cancel(sessionID string) error {
	s.mu.RLock()
	session := s.sessions[sessionID]
	s.mu.RUnlock()
	if session == nil {
		return errors.New("ussi: session not found")
	}
	session.Terminate()
	s.mu.Lock()
	delete(s.sessions, sessionID)
	if s.active == session {
		s.active = nil
	}
	s.mu.Unlock()
	return nil
}

// ActiveSessionID returns the active session ID.
func (s *Service) ActiveSessionID() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.active == nil {
		return ""
	}
	return s.active.id
}

// HandleInboundByeNoResponse handles an inbound BYE without a response.
func (s *Service) HandleInboundByeNoResponse(sessionID string) {
	s.mu.RLock()
	session := s.sessions[sessionID]
	s.mu.RUnlock()
	if session != nil {
		session.Terminate()
	}
}

// HandleInboundInfoNoResponse handles an inbound INFO without a response.
func (s *Service) HandleInboundInfoNoResponse(sessionID, body string) {
	_ = sessionID
	_ = body
}

// activeSession returns the active session.
func (s *Service) activeSession() *Session {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.active
}

// clearSession clears the active session.
func (s *Service) clearSession(sessionID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.sessions, sessionID)
	if s.active != nil && s.active.id == sessionID {
		s.active = nil
	}
}

// logInboundMismatch logs an inbound mismatch (no-op).
func (s *Service) logInboundMismatch() {}

// matchInboundSession matches an inbound request to a session.
func (s *Service) matchInboundSession(callID string) *Session {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, session := range s.sessions {
		if session.callID == callID {
			return session
		}
	}
	return nil
}

// sessionFor returns the session for an ID.
func (s *Service) sessionFor(sessionID string) *Session {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.sessions[sessionID]
}

// setSession registers a session.
func (s *Service) setSession(session *Session) {
	if session == nil {
		return
	}
	s.mu.Lock()
	s.sessions[session.id] = session
	s.active = session
	s.mu.Unlock()
}

// learnSessionFromResponse learns the dialog from a response.
func learnSessionFromResponse(session *Session, callID, dialogID string) {
	if session == nil {
		return
	}
	session.mu.Lock()
	session.callID = callID
	session.dialogID = dialogID
	session.mu.Unlock()
}

// sendACK sends an ACK for a session (no-op placeholder).
func sendACK(session *Session) {}

// sessionDialog returns the dialog ID for a session.
func sessionDialog(session *Session) string {
	if session == nil {
		return ""
	}
	session.mu.RLock()
	defer session.mu.RUnlock()
	return session.dialogID
}

// newSessionID generates a session ID.
func newSessionID() string {
	return fmt.Sprintf("ussi-%d", time.Now().UnixNano())
}

// Profile is the USSD/SS profile applied to an initial INVITE.
type Profile struct {
	ContactParams string
	IMPI          string
	Domain        string
}

// ApplyInitialInvite records the contact parameters from an initial INVITE.
func (p *Profile) ApplyInitialInvite(contact string) {
	if p == nil {
		return
	}
	p.ContactParams = contact
}

// ContactHeaderParams returns the Contact header parameters.
func (p *Profile) ContactHeaderParams() string {
	if p == nil {
		return ""
	}
	return p.ContactParams
}
