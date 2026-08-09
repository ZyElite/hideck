package swu

import (
	"context"
	"errors"
	"fmt"

	"github.com/iniwex5/vowifi-go/engine/crypto"
	"github.com/iniwex5/vowifi-go/engine/ikev2"
	"github.com/iniwex5/vowifi-go/engine/logger"
	"go.uber.org/zap"
)

func (s *Session) trySessionResumption(ctx context.Context) (bool, error) {
	ticket, oldSKd, complete := s.sessionResumptionCredentials()
	defer crypto.Wipe(ticket)
	defer crypto.Wipe(oldSKd)
	if len(ticket) == 0 && len(oldSKd) == 0 {
		return false, nil
	}
	if !complete {
		err := errors.New("swu: incomplete cached session resumption credentials")
		s.discardSessionResumptionCredentials()
		logger.Warn("SWu session resumption credentials rejected", zap.Error(err))
		return false, err
	}
	if err := s.performSessionResumptionContext(ctx); err != nil {
		s.resetAfterSessionResumeFailure()
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			logger.Warn("SWu session resumption canceled", zap.Error(err))
			return false, err
		}
		s.discardSessionResumptionCredentials()
		logger.Warn("SWu session resumption failed; continuing with full authentication", zap.Error(err))
		return false, err
	}
	logger.Info("SWu session resumed", zap.Int("ticket_length", len(ticket)))
	return true, nil
}

func (s *Session) sessionResumptionCredentials() (ticket, oldSKd []byte, complete bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	ticket = append([]byte(nil), s.resumeTicket...)
	oldSKd = append([]byte(nil), s.resumeOldSKd...)
	return ticket, oldSKd, len(ticket) > 0 && len(oldSKd) > 0
}

func (s *Session) discardSessionResumptionCredentials() {
	s.mu.Lock()
	ticket, oldSKd := s.resumeTicket, s.resumeOldSKd
	s.resumeTicket, s.resumeOldSKd = nil, nil
	s.mu.Unlock()
	crypto.Wipe(ticket)
	crypto.Wipe(oldSKd)
	if s.cfg != nil && s.cfg.OnTicketUpdate != nil {
		s.cfg.OnTicketUpdate(nil, nil)
	}
}

func (s *Session) resetAfterSessionResumeFailure() {
	s.clearIKEKeyMaterial()
	s.resetSessionResumeChildState()
	s.mu.Lock()
	crypto.Wipe(s.lastIKERequest)
	crypto.Wipe(s.lastIKEResponse)
	for _, packet := range s.lastIKERequestSet {
		crypto.Wipe(packet)
	}
	s.spiI, s.spiR = [8]byte{}, [8]byte{}
	s.SPIi, s.SPIr = 0, 0
	s.Ni, s.nr = nil, nil
	s.lastIKERequest, s.lastIKEResponse = nil, nil
	s.lastIKERequestSet = nil
	s.nextOutboundID = 1
	s.responderAuthenticated = false
	s.eapOnlyAuthentication = false
	s.eapOnlyRequested = false
	s.sessionResumed = false
	s.syncLegacyIKEStateLocked()
	s.mu.Unlock()
	s.fragmentBuf.clear()
}

func (s *Session) resetSessionResumeChildState() {
	s.childSAMu.Lock()
	childDH := s.childDH
	childSecret := s.childDHSecret
	s.espLocalSPI, s.espRemoteSPI = 0, 0
	s.childNi, s.childNr = nil, nil
	s.childDH, s.childDHSecret = nil, nil
	s.childTSi, s.childTSr = nil, nil
	s.childSAMu.Unlock()
	crypto.Wipe(childSecret)
	if childDH != nil {
		crypto.Wipe(childDH.SharedKey)
	}
}

func (s *Session) clearSessionResumptionMemory() {
	s.mu.Lock()
	ticket, oldSKd := s.resumeTicket, s.resumeOldSKd
	s.resumeTicket, s.resumeOldSKd = nil, nil
	s.mu.Unlock()
	crypto.Wipe(ticket)
	crypto.Wipe(oldSKd)
}

func joinSessionResumeFallbackError(resumeErr, handshakeErr error) error {
	if resumeErr == nil {
		return handshakeErr
	}
	return errors.Join(fmt.Errorf("swu: session resumption failed: %w", resumeErr), handshakeErr)
}

func (s *Session) captureSessionTicket(payloads []ikev2.Payload) error {
	ticket := lastSessionTicket(payloads)
	if len(ticket) == 0 {
		return nil
	}
	s.mu.RLock()
	var skd []byte
	if s.ikeKeys != nil {
		skd = append([]byte(nil), s.ikeKeys.SK_d...)
	}
	s.mu.RUnlock()
	if len(skd) == 0 {
		crypto.Wipe(ticket)
		return errors.New("swu: received TICKET_OPAQUE without active SK_d")
	}
	s.replaceSessionResumptionCredentials(ticket, skd)
	return nil
}

func lastSessionTicket(payloads []ikev2.Payload) []byte {
	var ticket []byte
	for _, payload := range payloads {
		notify, ok := payload.(*ikev2.EncryptedPayloadNotify)
		if ok && notify.NotifyType == ikev2.TICKET_OPAQUE && len(notify.NotifyData) > 0 {
			ticket = append(ticket[:0], notify.NotifyData...)
		}
	}
	return ticket
}

func (s *Session) replaceSessionResumptionCredentials(ticket, skd []byte) {
	storedTicket, storedSKd := append([]byte(nil), ticket...), append([]byte(nil), skd...)
	s.mu.Lock()
	oldTicket, oldSKd := s.resumeTicket, s.resumeOldSKd
	s.resumeTicket, s.resumeOldSKd = storedTicket, storedSKd
	s.mu.Unlock()
	crypto.Wipe(oldTicket)
	crypto.Wipe(oldSKd)
	if s.cfg != nil && s.cfg.OnTicketUpdate != nil {
		s.cfg.OnTicketUpdate(append([]byte(nil), storedTicket...), append([]byte(nil), storedSKd...))
	}
	crypto.Wipe(ticket)
	crypto.Wipe(skd)
}
