package imscore

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"
)

// registerSession tracks one registration attempt.
type registerSession struct {
	callID     string
	fromTag    string
	cseq       int
	branch     string
	challenge  *DigestChallenge
	authHeader string
	expires    time.Duration
	security   *securityAgreement
}

// Register performs the IMS registration flow (RFC 3261 + Digest-AKA).
func (s *Service) Register(ctx context.Context) error {
	s.registerMu.Lock()
	defer s.registerMu.Unlock()
	select {
	case <-s.stop:
		return errors.New("imscore: service stopped")
	default:
	}
	s.mu.Lock()
	s.regState = regRegistering
	s.mu.Unlock()

	expires, err := s.runRegisterFlow(ctx)
	if err != nil {
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
	s.scheduleRegistrationRefresh(expires)
	return nil
}

// runRegisterFlow drives the REGISTER -> 401/407 -> REGISTER flow.
func (s *Service) runRegisterFlow(ctx context.Context) (time.Duration, error) {
	if s.cfg == nil {
		return 0, errors.New("imscore: no configuration")
	}
	if err := s.ensureRegistrationTransport(ctx); err != nil {
		return 0, err
	}
	session, err := s.sessionForRegisterAttempt()
	if err != nil {
		return 0, err
	}
	s.mu.Lock()
	s.regSession = session
	s.mu.Unlock()

	// Initial REGISTER.
	req := s.buildRegister(session, "")
	if err := s.sendSIP(req); err != nil {
		return 0, fmt.Errorf("imscore: send initial REGISTER: %w", err)
	}

	// Wait for the challenge response.
	resp, err := s.receiveResponse(ctx, session)
	if err != nil {
		return 0, err
	}
	if resp.StatusCode == 401 || resp.StatusCode == 407 {
		challenge, err := s.extractChallenge(resp, resp.StatusCode)
		if err != nil {
			return 0, err
		}
		session.challenge = challenge

		// Build the authenticated REGISTER.
		auth, aka, err := s.buildAuthorizationWithResult(session)
		if err != nil {
			return 0, err
		}
		if session.security != nil {
			if err := s.installNegotiatedIPSec(session, resp, aka); err != nil {
				return 0, err
			}
		}
		session.cseq++
		req = s.buildRegister(session, auth)
		if err := s.sendSIP(req); err != nil {
			return 0, fmt.Errorf("imscore: send authenticated REGISTER: %w", err)
		}

		// Wait for the final response.
		resp, err = s.receiveResponse(ctx, session)
		if err != nil {
			return 0, err
		}
	}
	if session.security != nil && session.security.server == nil {
		return 0, errors.New("imscore: registration completed without 3GPP security agreement")
	}

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		expires := registrationExpires(resp, s.cfg.Expires)
		session.expires = expires
		return expires, nil
	}
	return 0, fmt.Errorf("imscore: registration failed with status %d", resp.StatusCode)
}

func (s *Service) sessionForRegisterAttempt() (*registerSession, error) {
	s.mu.RLock()
	previous := s.regSession
	if previous != nil && previous.expires > 0 {
		session := &registerSession{
			callID: previous.callID, fromTag: previous.fromTag,
			cseq: previous.cseq + 1, security: previous.security,
		}
		s.mu.RUnlock()
		return session, nil
	}
	s.mu.RUnlock()
	security, err := s.prepareSecurityAgreement()
	if err != nil {
		return nil, err
	}
	return &registerSession{callID: newCallID(), fromTag: newTag(), cseq: 1, security: security}, nil
}

// buildRegister builds a REGISTER request.
func (s *Service) buildRegister(session *registerSession, authHeader string) string {
	cfg := s.cfg
	// Each request starts a distinct SIP transaction even when refresh reuses
	// the registration Call-ID.
	session.branch = "z9hG4bK" + newBranch()
	expires := cfg.Expires
	if expires <= 0 {
		expires = 3600 * time.Second
	}
	var b strings.Builder
	b.WriteString(fmt.Sprintf("REGISTER sip:%s SIP/2.0\r\n", cfg.Domain))
	b.WriteString(fmt.Sprintf("Via: SIP/2.0/%s %s;branch=%s;rport\r\n", transportUpper(cfg.Transport), sipLocalAddress(cfg), session.branch))
	publicIdentity := primaryPublicIdentity(cfg)
	b.WriteString(fmt.Sprintf("From: <%s>;tag=%s\r\n", publicIdentity, session.fromTag))
	b.WriteString(fmt.Sprintf("To: <%s>\r\n", publicIdentity))
	b.WriteString(fmt.Sprintf("Call-ID: %s\r\n", session.callID))
	b.WriteString(fmt.Sprintf("CSeq: %d REGISTER\r\n", session.cseq))
	b.WriteString(fmt.Sprintf("Contact: <sip:%s@%s>;+sip.instance=\"urn:uuid:%s\";expires=%d\r\n", contactUser(cfg), s.contactAddress(session), cfg.DeviceID, int(expires.Seconds())))
	b.WriteString(fmt.Sprintf("Expires: %d\r\n", int(expires.Seconds())))
	b.WriteString("Max-Forwards: 70\r\n")
	b.WriteString("Supported: path, outbound\r\n")
	b.WriteString(registerSecurityHeaders(session))
	if authHeader != "" {
		b.WriteString("Authorization: " + authHeader + "\r\n")
	}
	b.WriteString("Content-Length: 0\r\n\r\n")
	return b.String()
}

func registerSecurityHeaders(session *registerSession) string {
	if session == nil || session.security == nil {
		return ""
	}
	var builder strings.Builder
	builder.WriteString("Security-Client: " + session.security.clientHeader + "\r\n")
	builder.WriteString("Require: sec-agree\r\n")
	builder.WriteString("Proxy-Require: sec-agree\r\n")
	if session.security.verifyHeader != "" {
		builder.WriteString("Security-Verify: " + session.security.verifyHeader + "\r\n")
	}
	return builder.String()
}

func (s *Service) contactAddress(session *registerSession) string {
	if session == nil || session.security == nil {
		return sipLocalAddress(s.cfg)
	}
	return net.JoinHostPort(s.cfg.LocalIP.String(), strconv.Itoa(int(session.security.client.PortS)))
}

func primaryPublicIdentity(cfg *IMSConfig) string {
	if len(cfg.IMPU) > 0 && strings.TrimSpace(cfg.IMPU[0]) != "" {
		identity := strings.TrimSpace(cfg.IMPU[0])
		if strings.HasPrefix(strings.ToLower(identity), "sip:") {
			return identity
		}
		return "sip:" + identity
	}
	return "sip:" + cfg.IMPI
}

func contactUser(cfg *IMSConfig) string {
	if strings.TrimSpace(cfg.IMSI) != "" {
		return strings.TrimSpace(cfg.IMSI)
	}
	user, _, _ := strings.Cut(strings.TrimPrefix(strings.TrimSpace(cfg.IMPI), "sip:"), "@")
	return user
}

func registrationExpires(resp *sipResponse, configured time.Duration) time.Duration {
	if seconds := contactExpires(resp.Header("Contact")); seconds > 0 {
		return time.Duration(seconds) * time.Second
	}
	if seconds, err := strconv.Atoi(strings.TrimSpace(resp.Header("Expires"))); err == nil && seconds > 0 {
		return time.Duration(seconds) * time.Second
	}
	if configured > 0 {
		return configured
	}
	return time.Hour
}

func contactExpires(contact string) int {
	for _, parameter := range strings.Split(contact, ";") {
		name, value, ok := strings.Cut(strings.TrimSpace(parameter), "=")
		if !ok || !strings.EqualFold(name, "expires") {
			continue
		}
		value, _, _ = strings.Cut(value, ",")
		seconds, err := strconv.Atoi(strings.TrimSpace(value))
		if err == nil && seconds > 0 {
			return seconds
		}
	}
	return 0
}

func (s *Service) scheduleRegistrationRefresh(expires time.Duration) {
	delay := expires - expires/5
	if delay <= 0 {
		delay = expires / 2
	}
	if delay <= 0 {
		delay = time.Second
	}
	s.mu.Lock()
	if s.refreshTimer != nil {
		s.refreshTimer.Stop()
	}
	s.refreshTimer = time.AfterFunc(delay, s.refreshRegistration)
	s.mu.Unlock()
}

func (s *Service) refreshRegistration() {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := s.Register(ctx); err != nil {
		select {
		case s.registerErrors <- fmt.Errorf("imscore: registration refresh failed: %w", err):
		default:
		}
	}
}

func sipLocalAddress(cfg *IMSConfig) string {
	port := cfg.LocalPort
	if port <= 0 {
		port = 5060
	}
	return net.JoinHostPort(cfg.LocalIP.String(), strconv.Itoa(port))
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
	authorization, _, err := s.buildAuthorizationWithResult(session)
	return authorization, err
}

func (s *Service) buildAuthorizationWithResult(session *registerSession) (string, AKAResult, error) {
	cfg := s.cfg
	if cfg.AKAProvider == nil {
		return "", AKAResult{}, errors.New("imscore: no AKA provider for digest")
	}
	if session.challenge == nil {
		return "", AKAResult{}, errors.New("imscore: no challenge for digest")
	}
	uri := "sip:" + cfg.Domain
	return ProcessAKAChallengeWithResult(session.challenge, cfg.AKAProvider, cfg.IMPI, "REGISTER", uri)
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

// receiveResponse waits for a response matching the full REGISTER transaction.
func (s *Service) receiveResponse(ctx context.Context, session *registerSession) (*sipResponse, error) {
	if s.transport == nil {
		return nil, errors.New("imscore: no SIP transport")
	}
	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case resp := <-s.transport.Responses():
			matched, err := matchesRegisterTransaction(resp, session)
			if err != nil {
				return nil, err
			}
			if !matched || resp.StatusCode < 200 {
				continue
			}
			return resp, nil
		}
	}
}

func matchesRegisterTransaction(resp *sipResponse, session *registerSession) (bool, error) {
	if resp == nil || resp.CallID != session.callID {
		return false, nil
	}
	cseq, method, err := parseSIPCSeq(resp.CSeq)
	if err != nil {
		return false, fmt.Errorf("imscore: invalid REGISTER response CSeq: %w", err)
	}
	if cseq != session.cseq || !strings.EqualFold(method, "REGISTER") {
		return false, nil
	}
	branch, err := parseTopViaBranch(resp.Header("Via"))
	if err != nil {
		return false, fmt.Errorf("imscore: invalid REGISTER response Via: %w", err)
	}
	return branch == session.branch, nil
}

func parseSIPCSeq(value string) (int, string, error) {
	fields := strings.Fields(value)
	if len(fields) != 2 {
		return 0, "", errors.New("expected sequence number and method")
	}
	sequence, err := strconv.Atoi(fields[0])
	if err != nil || sequence < 0 {
		return 0, "", fmt.Errorf("invalid sequence number %q", fields[0])
	}
	return sequence, fields[1], nil
}

func parseTopViaBranch(value string) (string, error) {
	topVia, _, _ := strings.Cut(value, ",")
	for _, parameter := range strings.Split(topVia, ";")[1:] {
		name, branch, ok := strings.Cut(strings.TrimSpace(parameter), "=")
		if ok && strings.EqualFold(name, "branch") && strings.TrimSpace(branch) != "" {
			return strings.TrimSpace(branch), nil
		}
	}
	return "", errors.New("missing branch parameter")
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
