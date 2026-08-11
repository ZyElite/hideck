package imscore

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/iniwex5/vowifi-go/internal/vowifi/logging"
)

const giffgaffCarrierPresetID = "giffgaff_23410"

var registrationCleanupAttempts sync.Map

// ClearRegistrationBindings removes every registrar binding for the current
// public identity. The caller must immediately register again on success.
func (s *Service) ClearRegistrationBindings(ctx context.Context) error {
	if s == nil {
		return errors.New("imscore: nil service")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	s.registerMu.Lock()
	defer s.registerMu.Unlock()
	return s.clearRegistrationBindingsLocked(ctx)
}

func (s *Service) clearRegistrationBindingsLocked(ctx context.Context) error {
	session, err := s.registrationBindingCleanupSession()
	if err != nil {
		return err
	}
	response, err := s.exchangeWildcardUnregister(ctx, session)
	if err != nil {
		return err
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("imscore: wildcard deregistration rejected with status %d %s",
			response.StatusCode, strings.TrimSpace(response.Reason))
	}
	s.mu.Lock()
	s.regSession = session
	s.mu.Unlock()
	logging.Info("IMS registrar bindings cleared", "device", s.DeviceID(), "cseq", session.cseq)
	return nil
}

func (s *Service) requestRegistrationBindingCleanup(document *regInfoDocument) bool {
	if s == nil || s.cfg == nil {
		return false
	}
	if !strings.EqualFold(strings.TrimSpace(s.cfg.CarrierPresetID), giffgaffCarrierPresetID) {
		return false
	}
	s.mu.RLock()
	contactID, contactNeedle := s.registrationContactIdentityLocked()
	cleanupKey := s.registrationBindingCleanupKeyLocked()
	s.mu.RUnlock()
	if cleanupKey == "" || !hasDuplicateActiveRegistration(document, contactID, contactNeedle) {
		return false
	}
	if _, attempted := registrationCleanupAttempts.LoadOrStore(cleanupKey, struct{}{}); attempted {
		return false
	}
	s.bindingCleanupPending.Store(true)
	logging.Info("IMS duplicate registration bindings require cleanup", "device", s.DeviceID())
	return true
}

func (s *Service) registrationBindingCleanupKeyLocked() string {
	if s.regSession == nil {
		return ""
	}
	publicID := strings.TrimSpace(s.regSession.publicID)
	if publicID == "" && s.cfg != nil {
		publicID = strings.TrimSpace(s.cfg.IMPU)
	}
	deviceID := strings.TrimSpace(s.DeviceID())
	if deviceID == "" || publicID == "" || strings.TrimSpace(s.regSession.authHeader) == "" {
		return ""
	}
	return deviceID + "\x00" + publicID
}

func hasDuplicateActiveRegistration(
	document *regInfoDocument,
	contactID string,
	contactNeedle string,
) bool {
	if document == nil || (contactID == "" && contactNeedle == "") {
		return false
	}
	for _, registration := range document.Registrations {
		activeCount := 0
		currentActive := false
		for _, contact := range registration.Contacts {
			state := strings.ToLower(strings.TrimSpace(contact.State))
			if state != "active" && state != "registered" {
				continue
			}
			activeCount++
			currentActive = currentActive || reginfoContactMatches(contact, contactID, contactNeedle)
		}
		if currentActive && activeCount > 1 {
			return true
		}
	}
	return false
}

func (s *Service) registrationBindingCleanupSession() (*registerSession, error) {
	s.mu.RLock()
	current := s.regSession
	if current == nil {
		s.mu.RUnlock()
		return nil, errors.New("imscore: no registered session to clear")
	}
	session := &registerSession{
		callID: current.callID, fromTag: current.fromTag,
		contactUser: current.contactUser, cseq: current.cseq + 1,
		challenge: current.challenge, authHeader: current.authHeader,
		expires: current.expires, security: current.security,
		publicID: current.publicID, serviceRoute: current.serviceRoute,
		path: current.path, template: current.template,
	}
	s.mu.RUnlock()
	if strings.TrimSpace(session.authHeader) == "" {
		return nil, errors.New("imscore: registered session has no authorization for binding cleanup")
	}
	return session, nil
}

func (s *Service) exchangeWildcardUnregister(
	ctx context.Context,
	session *registerSession,
) (*sipResponse, error) {
	s.recordRegisterSession(session)
	request := s.buildWildcardUnregister(session, session.authHeader)
	logging.Info("IMS wildcard deregistration outbound", "device", s.DeviceID(), "cseq", session.cseq)
	logging.RunDebug("IMS wildcard deregistration outbound", "cseq", session.cseq,
		"sip", logging.RedactSIPRaw(request))
	response, err := s.transport.RoundTrip(ctx, request)
	if err != nil {
		logging.Info("IMS wildcard deregistration transaction failed",
			"device", s.DeviceID(), "cseq", session.cseq, "err", err)
		return nil, fmt.Errorf("imscore: wildcard deregistration CSeq %d transaction: %w",
			session.cseq, err)
	}
	logging.Info("IMS wildcard deregistration response", "device", s.DeviceID(),
		"cseq", session.cseq, "status", response.StatusCode)
	matched, matchErr := matchesRegisterTransaction(response, session)
	if matchErr != nil {
		return nil, matchErr
	}
	if !matched {
		return nil, fmt.Errorf("imscore: wildcard deregistration CSeq %d received mismatched response",
			session.cseq)
	}
	s.recordRegisterResponse(response)
	return response, nil
}
