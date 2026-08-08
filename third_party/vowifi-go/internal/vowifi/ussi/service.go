package ussi

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"
)

const dialogFailureCleanupTimeout = 5 * time.Second

// Send starts a USSI dialog and waits for a real network result.
func (s *Service) Send(command string) (*Result, error) {
	return s.SendContext(context.Background(), command)
}

// SendContext starts a USSI dialog using the caller's timeout.
func (s *Service) SendContext(ctx context.Context, command string) (*Result, error) {
	command = strings.TrimSpace(command)
	if command == "" {
		return nil, errors.New("ussi: command is empty")
	}
	cfg, err := s.configForOperation()
	if err != nil {
		return nil, err
	}
	session, err := s.newSession(cfg, command)
	if err != nil {
		return nil, err
	}
	request, err := buildInitialInvite(cfg, session, command)
	if err != nil {
		s.clearSession(session.id)
		return nil, err
	}
	response, err := cfg.Transport.RoundTrip(ctx, request)
	if err != nil {
		s.clearSession(session.id)
		return nil, fmt.Errorf("ussi: INVITE transaction failed: %w", err)
	}
	learnDialog(session, response)
	if err := cfg.Transport.Send(ctx, buildACK(cfg, session, response.StatusCode)); err != nil {
		s.clearSession(session.id)
		return nil, fmt.Errorf("ussi: send ACK: %w", err)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		s.clearSession(session.id)
		return nil, fmt.Errorf("ussi: INVITE rejected: %d %s", response.StatusCode, strings.TrimSpace(response.Reason))
	}
	if result, found, parseErr := parseResponseResult(session.id, response); found || parseErr != nil {
		return s.finishNetworkResult(session, result, parseErr)
	}
	return s.waitResult(ctx, cfg, session)
}

// Continue sends an in-dialog INFO and waits for the network result.
func (s *Service) Continue(sessionID, input string) (*Result, error) {
	return s.ContinueContext(context.Background(), sessionID, input)
}

// ContinueContext continues a USSI dialog.
func (s *Service) ContinueContext(ctx context.Context, sessionID, input string) (*Result, error) {
	input = strings.TrimSpace(input)
	if input == "" {
		return nil, errors.New("ussi: input is empty")
	}
	cfg, err := s.configForOperation()
	if err != nil {
		return nil, err
	}
	session := s.sessionFor(sessionID)
	if session == nil || !session.IsActive() {
		return nil, errors.New("ussi: session not found")
	}
	body, err := requestXML(input)
	if err != nil {
		return nil, err
	}
	session.mu.Lock()
	session.cseq++
	request := buildDialogRequest(cfg, session, "INFO", body)
	session.mu.Unlock()
	response, err := cfg.Transport.RoundTrip(ctx, request)
	if err != nil {
		cause := fmt.Errorf("ussi: INFO transaction failed: %w", err)
		return nil, s.cleanupFailedDialog(cfg, session, cause)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		s.clearSession(session.id)
		return nil, fmt.Errorf("ussi: INFO rejected: %d %s", response.StatusCode, strings.TrimSpace(response.Reason))
	}
	if result, found, parseErr := parseResponseResult(session.id, response); found || parseErr != nil {
		return s.finishNetworkResult(session, result, parseErr)
	}
	return s.waitResult(ctx, cfg, session)
}

// Cancel sends a real in-dialog BYE before clearing the session.
func (s *Service) Cancel(sessionID string) error {
	return s.CancelContext(context.Background(), sessionID)
}

// CancelContext cancels a USSI dialog.
func (s *Service) CancelContext(ctx context.Context, sessionID string) error {
	cfg, err := s.configForOperation()
	if err != nil {
		return err
	}
	if strings.TrimSpace(sessionID) == "" {
		sessionID = s.ActiveSessionID()
	}
	session := s.sessionFor(sessionID)
	if session == nil {
		return errors.New("ussi: session not found")
	}
	session.mu.Lock()
	session.cseq++
	request := buildDialogRequest(cfg, session, "BYE", nil)
	session.mu.Unlock()
	response, roundTripErr := cfg.Transport.RoundTrip(ctx, request)
	s.clearSession(sessionID)
	if roundTripErr != nil {
		return fmt.Errorf("ussi: BYE transaction failed: %w", roundTripErr)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("ussi: BYE rejected: %d %s", response.StatusCode, strings.TrimSpace(response.Reason))
	}
	return nil
}

// ActiveSessionID returns the current network dialog identifier.
func (s *Service) ActiveSessionID() string {
	if s == nil {
		return ""
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.active == nil || !s.active.IsActive() {
		return ""
	}
	return s.active.ID()
}

// HandleInbound handles network INFO and BYE requests for an active dialog.
func (s *Service) HandleInbound(request InboundRequest) (InboundResult, error) {
	method := strings.ToUpper(strings.TrimSpace(request.Method))
	if method != "INFO" && method != "BYE" {
		return InboundResult{}, nil
	}
	looksUSSI := IsContentType(request.ContentType) || strings.EqualFold(strings.TrimSpace(request.InfoPackage), InfoPackage)
	session := s.matchInboundSession(request.CallID)
	if session == nil {
		if looksUSSI {
			return InboundResult{Handled: true, StatusCode: 481}, nil
		}
		return InboundResult{}, nil
	}
	if method == "BYE" && len(request.Body) == 0 {
		result := Result{SessionID: session.id, Code: "0", Done: true}
		s.deliverResult(session, result)
		s.clearSession(session.id)
		return InboundResult{Handled: true, StatusCode: 200, Result: &result}, nil
	}
	xmlBody, found, err := extractUSSI(request.ContentType, request.Body)
	if err != nil || !found {
		if err == nil {
			err = errors.New("ussi: inbound request has no USSI XML")
		}
		return InboundResult{Handled: true, StatusCode: 400}, err
	}
	result, err := resultFromXML(session.id, xmlBody)
	if err != nil {
		return InboundResult{Handled: true, StatusCode: 400}, err
	}
	if method == "BYE" {
		result.Done = true
		result.Code = "0"
	}
	s.deliverResult(session, result)
	if result.Done {
		s.clearSession(session.id)
	}
	return InboundResult{Handled: true, StatusCode: 200, Result: &result}, nil
}

// HandleInboundInfoNoResponse preserves the recovered public API.
func (s *Service) HandleInboundInfoNoResponse(sessionID, body string) {
	session := s.sessionFor(sessionID)
	if session == nil {
		return
	}
	_, _ = s.HandleInbound(InboundRequest{Method: "INFO", CallID: session.callID, ContentType: ContentType, Body: []byte(body)})
}

// HandleInboundByeNoResponse preserves the recovered public API.
func (s *Service) HandleInboundByeNoResponse(sessionID string) {
	session := s.sessionFor(sessionID)
	if session == nil {
		return
	}
	_, _ = s.HandleInbound(InboundRequest{Method: "BYE", CallID: session.callID})
}

// Stop terminates all sessions and wakes blocked callers.
func (s *Service) Stop() {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.stopped = true
	sessions := make([]*Session, 0, len(s.sessions))
	for _, session := range s.sessions {
		sessions = append(sessions, session)
	}
	s.sessions = make(map[string]*Session)
	s.active = nil
	s.mu.Unlock()
	for _, session := range sessions {
		session.Terminate()
		select {
		case session.results <- resultEvent{err: errors.New("ussi: service stopped")}:
		default:
		}
	}
}

func (s *Service) configForOperation() (Config, error) {
	if s == nil {
		return Config{}, errors.New("ussi: nil service")
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.stopped {
		return Config{}, errors.New("ussi: service stopped")
	}
	if s.cfg.Transport == nil {
		return Config{}, errors.New("ussi: service is not configured")
	}
	return s.cfg, nil
}

func (s *Service) newSession(cfg Config, command string) (*Session, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.active != nil && s.active.IsActive() {
		return nil, errors.New("ussi: another session is active")
	}
	id := "ussi-" + randomHex(8)
	session := &Session{
		id: id, callID: "ussi-" + token(id) + "@vowifi-go",
		localTag: randomHex(8), remoteURI: dialstringURI(command, cfg.Domain),
		inviteBranch: "z9hG4bK" + randomHex(12), cseq: 1, active: true,
		routeSet: splitHeaderValues(cfg.ServiceRoute), results: make(chan resultEvent, 4),
	}
	session.remoteTarget = session.remoteURI
	s.sessions[id] = session
	s.active = session
	return session, nil
}

func (s *Service) sessionFor(sessionID string) *Session {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.sessions[strings.TrimSpace(sessionID)]
}

func (s *Service) matchInboundSession(callID string) *Session {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, session := range s.sessions {
		if strings.TrimSpace(session.callID) == strings.TrimSpace(callID) && session.IsActive() {
			return session
		}
	}
	return nil
}

func (s *Service) clearSession(sessionID string) {
	s.mu.Lock()
	session := s.sessions[strings.TrimSpace(sessionID)]
	delete(s.sessions, strings.TrimSpace(sessionID))
	if s.active == session {
		s.active = nil
	}
	s.mu.Unlock()
	if session != nil {
		session.Terminate()
	}
}

func (s *Service) waitResult(ctx context.Context, cfg Config, session *Session) (*Result, error) {
	select {
	case <-ctx.Done():
		cause := fmt.Errorf("ussi: wait for network result: %w", ctx.Err())
		return nil, s.cleanupFailedDialog(cfg, session, cause)
	case event := <-session.results:
		if event.err != nil {
			return nil, event.err
		}
		return &event.result, nil
	}
}

func (s *Service) cleanupFailedDialog(cfg Config, session *Session, cause error) error {
	session.mu.Lock()
	session.cseq++
	request := buildDialogRequest(cfg, session, "BYE", nil)
	session.mu.Unlock()
	s.clearSession(session.id)

	ctx, cancel := context.WithTimeout(context.Background(), dialogFailureCleanupTimeout)
	defer cancel()
	response, err := cfg.Transport.RoundTrip(ctx, request)
	if err != nil {
		return errors.Join(cause, fmt.Errorf("ussi: cleanup BYE transaction failed: %w", err))
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		cleanupErr := fmt.Errorf("ussi: cleanup BYE rejected: %d %s", response.StatusCode, strings.TrimSpace(response.Reason))
		return errors.Join(cause, cleanupErr)
	}
	return cause
}

func (s *Service) finishNetworkResult(session *Session, result Result, err error) (*Result, error) {
	if err != nil {
		s.clearSession(session.id)
		return nil, err
	}
	s.notifyResult(result)
	if result.Done {
		s.clearSession(session.id)
	}
	return &result, nil
}

func (s *Service) deliverResult(session *Session, result Result) {
	s.notifyResult(result)
	select {
	case session.results <- resultEvent{result: result}:
	default:
	}
}

func (s *Service) notifyResult(result Result) {
	s.mu.RLock()
	onResult := s.cfg.OnResult
	s.mu.RUnlock()
	if onResult != nil {
		onResult(result)
	}
}

func parseResponseResult(sessionID string, response Response) (Result, bool, error) {
	if len(response.Body) == 0 {
		return Result{}, false, nil
	}
	body, found, err := extractUSSI(responseHeader(response.Headers, "Content-Type"), response.Body)
	if err != nil || !found {
		return Result{}, found, err
	}
	result, err := resultFromXML(sessionID, body)
	return result, true, err
}

func dialstringURI(command, domain string) string {
	var encoded strings.Builder
	for _, value := range []byte(command) {
		switch {
		case value >= '0' && value <= '9', value == '*':
			encoded.WriteByte(value)
		case value == '#':
			encoded.WriteString("%23")
		default:
			fmt.Fprintf(&encoded, "%%%02X", value)
		}
	}
	if strings.TrimSpace(domain) == "" {
		return "tel:" + encoded.String()
	}
	return "sip:" + encoded.String() + "@" + strings.TrimSpace(domain) + ";user=dialstring"
}

func randomHex(size int) string {
	data := make([]byte, size)
	if _, err := rand.Read(data); err != nil {
		panic("ussi: crypto/rand failed: " + err.Error())
	}
	return hex.EncodeToString(data)
}

func token(value string) string {
	return strings.NewReplacer("@", "", ".", "", "-", "").Replace(value)
}
