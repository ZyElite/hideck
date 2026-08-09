package swu

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"

	"github.com/iniwex5/vowifi-go/engine/crypto"
	"github.com/iniwex5/vowifi-go/engine/ikev2"
)

const (
	minimumIKENonceSize = 16
	maximumIKENonceSize = 256
)

// performSessionResumption restores the RFC 5723 ticket exchange.
func (s *Session) performSessionResumption() error {
	return s.performSessionResumptionContext(s.ctx)
}

func (s *Session) performSessionResumptionContext(ctx context.Context) error {
	s.ikeExchangeMu.Lock()
	defer s.ikeExchangeMu.Unlock()
	if err := s.prepareSessionResumeMaterial(); err != nil {
		return err
	}
	request, raw, err := s.buildSessionResumeRequest()
	if err != nil {
		return err
	}
	defer crypto.Wipe(raw)
	defer wipeSessionResumePayloads(request.Payloads)
	response, err := s.exchangeEstablishedRaw(ctx, request, [][]byte{raw})
	if err != nil {
		return fmt.Errorf("swu: exchange IKE_SESSION_RESUME: %w", err)
	}
	if err := s.handleIkeSessionResumeResp(response); err != nil {
		return err
	}
	s.setState(stateAuthenticating)
	return s.sendIkeAuthChildlessContext(ctx)
}

func (s *Session) prepareSessionResumeMaterial() error {
	ticket, oldSKd, ok := s.sessionResumptionCredentials()
	if !ok {
		return errors.New("swu: incomplete session resumption credentials")
	}
	crypto.Wipe(ticket)
	crypto.Wipe(oldSKd)
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.spiI == ([8]byte{}) {
		if _, err := rand.Read(s.spiI[:]); err != nil {
			return fmt.Errorf("swu: generate session resume SPIi: %w", err)
		}
		if s.spiI == ([8]byte{}) {
			return errors.New("swu: generated zero session resume SPIi")
		}
		s.SPIi = ikeSPIUint64(s.spiI)
	}
	if len(s.Ni) == 0 {
		s.Ni = make([]byte, s.nonceLen)
		if _, err := rand.Read(s.Ni); err != nil {
			return fmt.Errorf("swu: generate session resume nonce: %w", err)
		}
	}
	return validateIKENonce("initiator", s.Ni)
}

func (s *Session) buildSessionResumeRequest() (*ikev2.IKEPacket, []byte, error) {
	ticket, _, ok := s.sessionResumptionCredentials()
	if !ok {
		return nil, nil, errors.New("swu: session resumption credentials disappeared")
	}
	s.mu.RLock()
	spiI := s.spiI
	nonce := append([]byte(nil), s.Ni...)
	s.mu.RUnlock()
	request := &ikev2.IKEPacket{
		Header: newIKEHeader(spiI, [8]byte{}, ikev2.IKE_SESSION_RESUME, ikev2.FlagInitiator, 0),
		Payloads: []ikev2.Payload{
			&ikev2.EncryptedPayloadNonce{NonceData: nonce},
			&ikev2.EncryptedPayloadNotify{
				ProtocolID: 0, NotifyType: ikev2.TICKET_OPAQUE, NotifyData: ticket,
			},
		},
	}
	raw, err := request.Encode()
	if err != nil {
		crypto.Wipe(ticket)
		return nil, nil, fmt.Errorf("swu: encode IKE_SESSION_RESUME: %w", err)
	}
	return request, raw, nil
}

func wipeSessionResumePayloads(payloads []ikev2.Payload) {
	for _, payload := range payloads {
		notify, ok := payload.(*ikev2.EncryptedPayloadNotify)
		if ok && notify.NotifyType == ikev2.TICKET_OPAQUE {
			crypto.Wipe(notify.NotifyData)
		}
	}
}

// handleIkeSessionResumeResp restores the recovered raw-response boundary.
func (s *Session) handleIkeSessionResumeResp(data []byte) error {
	packet, err := ikev2.DecodePacket(data)
	if err != nil {
		return fmt.Errorf("swu: decode IKE_SESSION_RESUME response: %w", err)
	}
	if err := s.validateSessionResumeHeader(packet.Header, len(data)); err != nil {
		return err
	}
	responderNonce, err := sessionResumeNonce(packet.Payloads)
	if err != nil {
		return err
	}
	responderSPI := ikeSPIBytes(packet.Header.SPIr)
	keys, err := s.deriveSessionResumeKeys(responderSPI, responderNonce)
	if err != nil {
		return err
	}
	if err := s.ensureWiresharkDebugger(); err != nil {
		wipeIKEKeys(keys)
		return err
	}
	s.installSessionResumeKeys(responderSPI, responderNonce, keys)
	return nil
}

func (s *Session) validateSessionResumeHeader(header *ikev2.IKEHeader, packetLength int) error {
	if header == nil || header.ExchangeType != ikev2.IKE_SESSION_RESUME {
		return errors.New("swu: response is not IKE_SESSION_RESUME")
	}
	if header.Version>>4 != 2 || header.MessageID != 0 ||
		header.Flags&ikeResponseFlag == 0 || header.Flags&ikeInitiatorFlag != 0 ||
		header.Length != uint32(packetLength) {
		return errors.New("swu: invalid IKE_SESSION_RESUME response header")
	}
	s.mu.RLock()
	expectedSPI := ikeSPIUint64(s.spiI)
	s.mu.RUnlock()
	if header.SPIi != expectedSPI || header.SPIr == 0 {
		return errors.New("swu: IKE_SESSION_RESUME response SPI mismatch")
	}
	return nil
}

func sessionResumeNonce(payloads []ikev2.Payload) ([]byte, error) {
	var nonce []byte
	for _, payload := range payloads {
		switch value := payload.(type) {
		case *ikev2.EncryptedPayloadNonce:
			if nonce != nil {
				return nil, errors.New("swu: duplicate responder nonce in IKE_SESSION_RESUME")
			}
			nonce = append([]byte(nil), value.NonceData...)
		case *ikev2.EncryptedPayloadNotify:
			if err := sessionResumeNotifyError(value); err != nil {
				return nil, err
			}
		}
	}
	if err := validateIKENonce("responder", nonce); err != nil {
		return nil, err
	}
	return nonce, nil
}

func sessionResumeNotifyError(notify *ikev2.EncryptedPayloadNotify) error {
	switch notify.NotifyType {
	case ikev2.NO_PROPOSAL_CHOSEN:
		return errors.New("swu: IKE_SESSION_RESUME rejected with NO_PROPOSAL_CHOSEN")
	case ikev2.TICKET_NACK:
		return errors.New("swu: IKE_SESSION_RESUME rejected with TICKET_NACK")
	default:
		if notify.NotifyType < 16384 {
			return fmt.Errorf("swu: IKE_SESSION_RESUME rejected with %s (%d)",
				ikev2.NotifyTypeToString(notify.NotifyType), notify.NotifyType)
		}
		return nil
	}
}

func validateIKENonce(role string, nonce []byte) error {
	if len(nonce) < minimumIKENonceSize || len(nonce) > maximumIKENonceSize {
		return fmt.Errorf("swu: %s IKE nonce length %d is outside %d..%d",
			role, len(nonce), minimumIKENonceSize, maximumIKENonceSize)
	}
	return nil
}

func (s *Session) deriveSessionResumeKeys(responderSPI [8]byte, responderNonce []byte) (*IKEKeys, error) {
	s.mu.RLock()
	initiatorNonce := append([]byte(nil), s.Ni...)
	initiatorSPI := s.spiI
	oldSKd := append([]byte(nil), s.resumeOldSKd...)
	prf := s.prf
	s.mu.RUnlock()
	defer crypto.Wipe(oldSKd)
	if prf == nil || len(oldSKd) == 0 {
		return nil, errors.New("swu: no previous PRF or SK_d for session resumption")
	}
	seedInput := append(append([]byte(nil), initiatorNonce...), responderNonce...)
	skeyseed := prf.Compute(oldSKd, seedInput)
	crypto.Wipe(seedInput)
	defer crypto.Wipe(skeyseed)
	// The original v1.5.5 binary uses Nr | Ni | SPIi | SPIr for this prf+ seed.
	keySeed := append(append([]byte(nil), responderNonce...), initiatorNonce...)
	keySeed = append(keySeed, initiatorSPI[:]...)
	keySeed = append(keySeed, responderSPI[:]...)
	defer crypto.Wipe(keySeed)
	return s.deriveIKEKeys(skeyseed, keySeed, crypto.PRFOutputSize(prf))
}

func (s *Session) installSessionResumeKeys(responderSPI [8]byte, responderNonce []byte, keys *IKEKeys) {
	s.mu.Lock()
	previous := s.ikeKeys
	s.spiR = responderSPI
	s.nr = append([]byte(nil), responderNonce...)
	s.ikeKeys = keys
	s.nextOutboundID = 1
	s.syncLegacyIKEStateLocked()
	s.mu.Unlock()
	s.logIKEKeys(keys)
	if previous != nil && previous != keys {
		wipeIKEKeys(previous)
	}
}

// sendIkeAuthChildless requests the resumed CHILD_SA and configuration.
func (s *Session) sendIkeAuthChildless() error {
	return s.sendIkeAuthChildlessContext(s.ctx)
}

func (s *Session) sendIkeAuthChildlessContext(ctx context.Context) error {
	payloads, err := s.buildIKEAuthInitPayloads()
	if err != nil {
		return err
	}
	response, err := s.sendEncryptedWithRetryContext(ctx, payloads, ikev2.IKE_AUTH)
	if err != nil {
		return fmt.Errorf("swu: resumed IKE_AUTH exchange: %w", err)
	}
	packet, err := ikev2.DecodePacket(response)
	if err != nil {
		return fmt.Errorf("swu: decode resumed IKE_AUTH response: %w", err)
	}
	decrypted, err := s.decryptAndParse(packet)
	if err != nil {
		return fmt.Errorf("swu: authenticate resumed IKE_AUTH response: %w", err)
	}
	if hasPayloadType(decrypted, ikev2.PayloadEAP) {
		return errors.New("swu: resumed IKE_AUTH unexpectedly requested EAP")
	}
	if err := s.handleResumedIKEAuthPayloads(decrypted); err != nil {
		return err
	}
	s.mu.Lock()
	s.responderAuthenticated = true
	s.sessionResumed = true
	s.mu.Unlock()
	return nil
}

func (s *Session) handleResumedIKEAuthPayloads(payloads []ikev2.Payload) error {
	if err := ikeAuthenticationError(payloads); err != nil {
		return err
	}
	return s.applyFinalIKEAuthPayloads(payloads)
}
