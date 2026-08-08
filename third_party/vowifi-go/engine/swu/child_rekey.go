package swu

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"

	"github.com/iniwex5/vowifi-go/engine/ikev2"
	"github.com/iniwex5/vowifi-go/engine/ipsec"
)

type childSARuntime struct {
	outbound, inbound   *ipsec.SecurityAssociation
	localSPI, remoteSPI uint32
	ni, nr              []byte
	tsi, tsr            *ikev2.EncryptedPayloadTS
	outboundKeys        childDirectionKeys
}

func (s *Session) performChildSARekey(ctx context.Context) error {
	s.ikeExchangeMu.Lock()
	defer s.ikeExchangeMu.Unlock()
	if s.socket == nil || s.ikeKeys == nil || s.State() != stateEstablished {
		return errors.New("swu: session not established")
	}
	ni, localSPI, err := s.newChildSAInitiatorMaterial()
	if err != nil {
		return err
	}
	tsi, tsr := s.currentChildSelectors()
	packet := s.buildChildSARekeyRequest(localSPI, ni, tsi, tsr)
	payloads, err := s.exchangeEstablishedIKE(ctx, packet)
	if err != nil {
		return err
	}
	selection, err := validateChildSAResponse(payloads, childSAOffer{
		encryption: s.espCipher, encryptionKeyBits: s.espEncKeyBits, integrity: s.espInteg,
		tsi: tsi, tsr: tsr, localIPs: configuredInnerIPs(s),
		requireSA: true, requireNonce: true,
	})
	if err != nil {
		return err
	}
	runtime, err := s.prepareChildSARuntime(
		localSPI, selection.remoteSPI, ni, selection.nonce,
		selection.tsi, selection.tsr, true,
	)
	if err != nil {
		return err
	}
	s.installChildSARuntime(runtime)
	return nil
}

func (s *Session) newChildSAInitiatorMaterial() ([]byte, uint32, error) {
	nonce := make([]byte, s.nonceLen)
	if _, err := rand.Read(nonce); err != nil {
		return nil, 0, fmt.Errorf("generate CHILD_SA nonce: %w", err)
	}
	spi, err := randomChildSPI()
	if err != nil {
		return nil, 0, err
	}
	return nonce, spi, nil
}

func (s *Session) buildChildSARekeyRequest(localSPI uint32, nonce []byte, tsi, tsr *ikev2.EncryptedPayloadTS) *ikev2.IKEPacket {
	s.childSAMu.RLock()
	oldLocalSPI := s.espLocalSPI
	s.childSAMu.RUnlock()
	return &ikev2.IKEPacket{
		Header: newIKEHeader(
			s.SPIi, s.SPIr, ikev2.CREATE_CHILD_SA, s.localIKEFlags(false), s.nextMessageID(),
		),
		Payloads: []ikev2.Payload{
			&ikev2.EncryptedPayloadNotify{
				ProtocolID: ikev2.ProtoESP,
				NotifyType: ikev2.NotifyTypeRekeySA, SPI: spiBytes(oldLocalSPI),
			},
			&ikev2.EncryptedPayloadSA{Proposals: buildESPProposalsForSession(s, localSPI)},
			&ikev2.EncryptedPayloadNonce{NonceData: append([]byte(nil), nonce...)},
			cloneTrafficSelectorPayload(tsi),
			cloneTrafficSelectorPayload(tsr),
		},
	}
}

func (s *Session) exchangeEstablishedIKE(ctx context.Context, packet *ikev2.IKEPacket) ([]ikev2.Payload, error) {
	raw, err := s.encryptAndWrap(packet)
	if err != nil {
		return nil, err
	}
	if err := s.sendIKE(raw); err != nil {
		return nil, err
	}
	response, err := s.receiveIKE(ctx)
	if err != nil {
		return nil, err
	}
	payloads, err := s.decryptAndParse(response)
	if err != nil {
		return nil, err
	}
	if err := ikeAuthenticationError(payloads); err != nil {
		return nil, err
	}
	return payloads, nil
}

func (s *Session) currentChildSelectors() (*ikev2.EncryptedPayloadTS, *ikev2.EncryptedPayloadTS) {
	s.childSAMu.RLock()
	defer s.childSAMu.RUnlock()
	return cloneTrafficSelectorPayload(s.childTSi), cloneTrafficSelectorPayload(s.childTSr)
}

func (s *Session) prepareChildSARuntime(
	localSPI, remoteSPI uint32,
	initiatorNonce, responderNonce []byte,
	tsi, tsr *ikev2.EncryptedPayloadTS,
	localInitiator bool,
) (*childSARuntime, error) {
	keys, err := s.deriveChildSAKeysFor(initiatorNonce, responderNonce)
	if err != nil {
		return nil, err
	}
	outboundKeys, inboundKeys := keys.initiator, keys.responder
	if !localInitiator {
		outboundKeys, inboundKeys = keys.responder, keys.initiator
	}
	outbound, err := s.newESPAssociation(remoteSPI, outboundKeys)
	if err != nil {
		return nil, err
	}
	inbound, err := s.newESPAssociation(localSPI, inboundKeys)
	if err != nil {
		return nil, err
	}
	return &childSARuntime{
		outbound: outbound, inbound: inbound,
		localSPI: localSPI, remoteSPI: remoteSPI,
		ni:           append([]byte(nil), initiatorNonce...),
		nr:           append([]byte(nil), responderNonce...),
		tsi:          cloneTrafficSelectorPayload(tsi),
		tsr:          cloneTrafficSelectorPayload(tsr),
		outboundKeys: outboundKeys,
	}, nil
}

func (s *Session) installChildSARuntime(runtime *childSARuntime) {
	s.childSAMu.Lock()
	defer s.childSAMu.Unlock()
	s.espOutboundSA = runtime.outbound
	s.espInboundSA = runtime.inbound
	s.espLocalSPI = runtime.localSPI
	s.espRemoteSPI = runtime.remoteSPI
	s.childNi = append([]byte(nil), runtime.ni...)
	s.childNr = append([]byte(nil), runtime.nr...)
	s.childTSi = cloneTrafficSelectorPayload(runtime.tsi)
	s.childTSr = cloneTrafficSelectorPayload(runtime.tsr)
	s.espKey = append([]byte(nil), runtime.outboundKeys.enc...)
	s.espIntegKey = append([]byte(nil), runtime.outboundKeys.integ...)
}

func (s *Session) handlePeerChildSARekey(packet *ikev2.IKEPacket) error {
	payloads, err := s.decryptAndParse(packet)
	if err != nil {
		return err
	}
	return s.handlePeerChildSARekeyPayloads(packet, payloads)
}

func (s *Session) handlePeerChildSARekeyPayloads(packet *ikev2.IKEPacket, payloads []ikev2.Payload) error {
	if err := s.validatePeerRekeyNotify(payloads); err != nil {
		return err
	}
	currentTSi, currentTSr := s.currentChildSelectors()
	peerTSi := retypeTrafficSelectorPayload(currentTSr, ikev2.PayloadTSi)
	peerTSr := retypeTrafficSelectorPayload(currentTSi, ikev2.PayloadTSr)
	selection, err := validateChildSAResponse(payloads, childSAOffer{
		encryption: s.espCipher, encryptionKeyBits: s.espEncKeyBits, integrity: s.espInteg,
		tsi: peerTSi, tsr: peerTSr,
		requireSA: true, requireNonce: true,
	})
	if err != nil {
		return err
	}
	if !selectorsContainAnyIP(selection.tsr, configuredInnerIPs(s)) {
		return errors.New("swu: peer CHILD_SA TSr does not contain an assigned inner address")
	}
	localNonce, localSPI, err := s.newChildSAInitiatorMaterial()
	if err != nil {
		return err
	}
	runtime, err := s.prepareChildSARuntime(
		localSPI, selection.remoteSPI, selection.nonce, localNonce,
		retypeTrafficSelectorPayload(selection.tsr, ikev2.PayloadTSi),
		retypeTrafficSelectorPayload(selection.tsi, ikev2.PayloadTSr), false,
	)
	if err != nil {
		return err
	}
	responsePayloads := []ikev2.Payload{
		&ikev2.EncryptedPayloadSA{Proposals: buildESPProposalsForSession(s, localSPI)},
		&ikev2.EncryptedPayloadNonce{NonceData: append([]byte(nil), localNonce...)},
		cloneTrafficSelectorPayload(selection.tsi),
		cloneTrafficSelectorPayload(selection.tsr),
	}
	if err := s.sendEstablishedIKEResponse(packet, responsePayloads); err != nil {
		return err
	}
	s.installChildSARuntime(runtime)
	return nil
}

func retypeTrafficSelectorPayload(payload *ikev2.EncryptedPayloadTS, payloadType ikev2.PayloadType) *ikev2.EncryptedPayloadTS {
	cloned := cloneTrafficSelectorPayload(payload)
	if cloned != nil {
		cloned.IsInitiator = payloadType == ikev2.TSI
	}
	return cloned
}

func (s *Session) validatePeerRekeyNotify(payloads []ikev2.Payload) error {
	s.childSAMu.RLock()
	expectedSPI := s.espRemoteSPI
	s.childSAMu.RUnlock()
	for _, payload := range payloads {
		if payload == nil || payload.Type() != ikev2.PayloadNotify {
			continue
		}
		notify, ok := payload.(*ikev2.EncryptedPayloadNotify)
		if !ok || notify.ProtocolID != ikev2.ProtoESP || len(notify.SPI) != 4 ||
			notify.NotifyType != ikev2.NotifyTypeRekeySA {
			continue
		}
		if binary.BigEndian.Uint32(notify.SPI) != expectedSPI {
			return errors.New("swu: peer REKEY_SA identifies an unknown ESP SPI")
		}
		return nil
	}
	return errors.New("swu: peer CHILD_SA rekey missing REKEY_SA notification")
}

func (s *Session) sendEstablishedIKEResponse(request *ikev2.IKEPacket, payloads []ikev2.Payload) error {
	requestHeader := packetIKEHeader(request)
	response := &ikev2.IKEPacket{
		Header: &ikev2.IKEHeader{
			SPIi: requestHeader.SPIi, SPIr: requestHeader.SPIr, Version: requestHeader.Version,
			ExchangeType: requestHeader.ExchangeType, Flags: s.localIKEFlags(true),
			MessageID: requestHeader.MessageID,
		},
		Payloads: payloads,
	}
	raw, err := s.encryptAndWrap(response)
	if err != nil {
		return err
	}
	if s.socket == nil {
		return errors.New("swu: no IKE transport")
	}
	s.socket.SendIKE(raw)
	return nil
}

func (s *Session) handlePeerInformational(packet *ikev2.IKEPacket) error {
	payloads, err := s.decryptAndParse(packet)
	if err != nil {
		return err
	}
	var activeDelete bool
	for _, payload := range payloads {
		deletion, ok := payload.(*ikev2.EncryptedPayloadDelete)
		if !ok || deletion.ProtocolID != ikev2.ProtoESP {
			continue
		}
		activeDelete = activeDelete || s.deleteContainsCurrentChildSA(deletion.SPIs)
	}
	if err := s.sendEstablishedIKEResponse(packet, nil); err != nil {
		return err
	}
	if activeDelete {
		return errors.New("swu: peer deleted the active CHILD_SA")
	}
	return nil
}

func (s *Session) deleteContainsCurrentChildSA(spis []byte) bool {
	s.childSAMu.RLock()
	current := s.espRemoteSPI
	s.childSAMu.RUnlock()
	for len(spis) >= 4 {
		if binary.BigEndian.Uint32(spis[:4]) == current {
			return true
		}
		spis = spis[4:]
	}
	return false
}
