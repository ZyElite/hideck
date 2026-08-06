package imscore

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

// registerSession tracks one registration attempt.
type registerSession struct {
	callID    string
	fromTag   string
	cseq      int
	challenge *DigestChallenge
	authHeader string
	expires   time.Duration
}

// Register performs the IMS registration flow (RFC 3261 + Digest-AKA).
func (s *Service) Register(ctx context.Context) error {
	s.mu.Lock()
	s.regState = regRegistering
	s.mu.Unlock()

	if err := s.runRegisterFlow(ctx); err != nil {
		s.mu.Lock()
		s.regState = regFailed
		s.mu.Unlock()
		return err
	}
	s.mu.Lock()
	s.regState = regRegistered
	s.mu.Unlock()
	if s.onRegistered != nil {
		s.onRegistered()
	}
	return nil
}

// runRegisterFlow drives the REGISTER -> 401/407 -> REGISTER flow.
func (s *Service) runRegisterFlow(ctx context.Context) error {
	if s.cfg == nil {
		return errors.New("imscore: no configuration")
	}
	session := &registerSession{
		callID:  newCallID(),
		fromTag: newTag(),
		cseq:    1,
	}
	s.mu.Lock()
	s.regSession = session
	s.mu.Unlock()

	// Initial REGISTER.
	req := s.buildRegister(session, "")
	if err := s.sendSIP(req); err != nil {
		return fmt.Errorf("imscore: send initial REGISTER: %w", err)
	}

	// Wait for the challenge response.
	resp, err := s.receiveResponse(ctx, session.callID)
	if err != nil {
		return err
	}
	if resp.StatusCode == 401 || resp.StatusCode == 407 {
		challenge, err := s.extractChallenge(resp, resp.StatusCode)
		if err != nil {
			return err
		}
		session.challenge = challenge

		// Build the authenticated REGISTER.
		auth, err := s.buildAuthorization(session)
		if err != nil {
			return err
		}
		session.cseq++
		req = s.buildRegister(session, auth)
		if err := s.sendSIP(req); err != nil {
			return fmt.Errorf("imscore: send authenticated REGISTER: %w", err)
		}

		// Wait for the final response.
		resp, err = s.receiveResponse(ctx, session.callID)
		if err != nil {
			return err
		}
	}

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil
	}
	return fmt.Errorf("imscore: registration failed with status %d", resp.StatusCode)
}

// buildRegister builds a REGISTER request.
func (s *Service) buildRegister(session *registerSession, authHeader string) string {
	cfg := s.cfg
	expires := cfg.Expires
	if expires <= 0 {
		expires = 3600 * time.Second
	}
	var b strings.Builder
	b.WriteString(fmt.Sprintf("REGISTER sip:%s SIP/2.0\r\n", cfg.Domain))
	b.WriteString(fmt.Sprintf("Via: SIP/2.0/%s %s;branch=z9hG4bK%s;rport\r\n", transportUpper(cfg.Transport), formatHostPort(cfg.LocalIP), newBranch()))
	b.WriteString(fmt.Sprintf("From: <sip:%s@%s>;tag=%s\r\n", cfg.IMPI, cfg.Domain, session.fromTag))
	b.WriteString(fmt.Sprintf("To: <sip:%s@%s>\r\n", cfg.IMPI, cfg.Domain))
	b.WriteString(fmt.Sprintf("Call-ID: %s\r\n", session.callID))
	b.WriteString(fmt.Sprintf("CSeq: %d REGISTER\r\n", session.cseq))
	b.WriteString(fmt.Sprintf("Contact: <sip:%s@%s>;+sip.instance=\"urn:uuid:%s\"\r\n", cfg.IMPI, formatHostPort(cfg.LocalIP), cfg.DeviceID))
	b.WriteString(fmt.Sprintf("Expires: %d\r\n", int(expires.Seconds())))
	b.WriteString("Max-Forwards: 70\r\n")
	b.WriteString("Supported: path, outbound\r\n")
	if authHeader != "" {
		b.WriteString("Authorization: " + authHeader + "\r\n")
	}
	b.WriteString("Content-Length: 0\r\n\r\n")
	return b.String()
}

// extractChallenge extracts the digest challenge from a 401/407 response.
func (s *Service) extractChallenge(resp *sipResponse, statusCode int) (*DigestChallenge, error) {
	header := "WWW-Authenticate"
	if statusCode == 407 {
		header = "Proxy-Authenticate"
	}
	value := resp.Header(header)
	if value == "" {
		return nil, errors.New("imscore: challenge response missing " + header)
	}
	return ParseDigestChallenge(value)
}

// buildAuthorization computes the Authorization header for the session.
func (s *Service) buildAuthorization(session *registerSession) (string, error) {
	cfg := s.cfg
	if cfg.AKAProvider == nil {
		return "", errors.New("imscore: no AKA provider for digest")
	}
	if session.challenge == nil {
		return "", errors.New("imscore: no challenge for digest")
	}
	uri := "sip:" + cfg.Domain
	return ProcessAKAChallenge(session.challenge, cfg.AKAProvider, cfg.IMPI, "REGISTER", uri)
}

// ForceRegistered marks the service as registered (for tests).
func (s *Service) ForceRegistered() {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.regState = regRegistered
	s.mu.Unlock()
}

// Transport returns the SIP transport (for tests and wiring).
func (s *Service) Transport() *sipTransport {
	if s == nil {
		return nil
	}
	return s.transport
}

// SendRawSIP sends a raw SIP request through the transport (used by the
// voice layer for INVITE/BYE/ACK/CANCEL).
func (s *Service) SendRawSIP(req string) error {
	if s == nil {
		return errors.New("imscore: nil service")
	}
	return s.sendSIP(req)
}

// sendSIP sends a SIP request through the transport.
func (s *Service) sendSIP(req string) error {
	if s.transport == nil {
		return errors.New("imscore: no SIP transport")
	}
	return s.transport.Send(req)
}

// receiveResponse waits for a response matching the call ID.
func (s *Service) receiveResponse(ctx context.Context, callID string) (*sipResponse, error) {
	if s.transport == nil {
		return nil, errors.New("imscore: no SIP transport")
	}
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case resp := <-s.transport.Responses():
		if resp.CallID == callID {
			return resp, nil
		}
		return s.receiveResponse(ctx, callID)
	}
}

// newCallID generates a call ID.
func newCallID() string {
	return fmt.Sprintf("%s-%d", randomHex(8), time.Now().UnixNano()%100000)
}

// newTag generates a tag.
func newTag() string {
	return randomHex(8)
}

// newBranch generates a Via branch.
func newBranch() string {
	return randomHex(16)
}

// randomHex generates a hex string of n random bytes.
func randomHex(n int) string {
	const digits = "0123456789abcdef"
	b := make([]byte, n)
	_, _ = randRead(b)
	for i := range b {
		b[i] = digits[int(b[i])%16]
	}
	return string(b)
}

// transportUpper upper-cases a transport token.
func transportUpper(t string) string {
	switch strings.ToLower(strings.TrimSpace(t)) {
	case "tcp":
		return "TCP"
	case "tls":
		return "TLS"
	default:
		return "UDP"
	}
}

// formatHostPort formats an IP:port for SIP.
func formatHostPort(ip interface{ String() string }) string {
	return ip.String()
}
