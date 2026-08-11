package swu

import (
	"encoding/binary"
	"errors"

	enginecrypto "github.com/iniwex5/vowifi-go/engine/crypto"
	"github.com/iniwex5/vowifi-go/engine/ikev2"
	"github.com/iniwex5/vowifi-go/engine/ipsec"
	"github.com/iniwex5/vowifi-go/engine/logger"
	"go.uber.org/zap"
)

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
		dhGroup: s.currentChildDHGroup(), esn: s.espESN, tsi: peerTSi, tsr: peerTSr,
		requireSA: true, requireNonce: true,
	})
	if err != nil {
		return err
	}
	if !selectorsContainAnyIP(selection.tsr, configuredInnerIPs(s)) {
		return errors.New("swu: peer CHILD_SA TSr does not contain an assigned inner address")
	}
	return s.answerPeerChildSARekey(packet, payloads, selection)
}

func (s *Session) answerPeerChildSARekey(
	packet *ikev2.IKEPacket,
	payloads []ikev2.Payload,
	selection *childSASelection,
) error {
	localNonce, localSPI, err := s.newChildSAInitiatorMaterial()
	if err != nil {
		return err
	}
	dh, sharedSecret, err := preparePeerChildRekeyDH(payloads, selection.dhGroup)
	if err != nil {
		return err
	}
	runtime, err := s.prepareChildSARuntime(childSARuntimeSpec{
		localSPI: localSPI, remoteSPI: selection.remoteSPI,
		initiatorNonce: selection.nonce, responderNonce: localNonce,
		sharedSecret: sharedSecret, dh: dh,
		tsi: retypeTrafficSelectorPayload(selection.tsr, ikev2.PayloadTSi),
		tsr: retypeTrafficSelectorPayload(selection.tsi, ikev2.PayloadTSr), localInitiator: false,
	})
	if err != nil {
		if dh != nil {
			enginecrypto.Wipe(dh.SharedKey)
		}
		return err
	}
	proposals := buildESPProposalsForSession(s, localSPI)
	if dh != nil {
		proposals[0].AddTransform(ikev2.TransformTypeDH, ikev2.AlgorithmType(dh.Group), 0)
	}
	response := []ikev2.Payload{
		&ikev2.EncryptedPayloadSA{Proposals: proposals},
		&ikev2.EncryptedPayloadNonce{NonceData: append([]byte(nil), localNonce...)},
	}
	if dh != nil {
		response = append(response, &ikev2.EncryptedPayloadKE{
			DHGroup: ikev2.AlgorithmType(dh.Group), KEData: dh.PublicKeyBytes(),
		})
	}
	response = append(response, cloneTrafficSelectorPayload(selection.tsi), cloneTrafficSelectorPayload(selection.tsr))
	if err := s.sendEstablishedIKEResponse(packet, response); err != nil {
		wipeChildSARuntime(runtime)
		return err
	}
	s.childSAMu.RLock()
	oldLocalSPI, oldRemoteSPI := s.espLocalSPI, s.espRemoteSPI
	s.childSAMu.RUnlock()
	if err := s.activateChildSARuntime(runtime); err != nil {
		return err
	}
	logger.Info("peer CHILD_SA rekey installed",
		zap.Uint32("old_local_spi", oldLocalSPI), zap.Uint32("old_remote_spi", oldRemoteSPI),
		zap.Uint32("new_local_spi", runtime.localSPI), zap.Uint32("new_remote_spi", runtime.remoteSPI))
	s.markChildRekeyComplete()
	s.recordRetiredChildSA(oldRemoteSPI, oldLocalSPI)
	return nil
}

func preparePeerChildRekeyDH(
	payloads []ikev2.Payload,
	group uint16,
) (*enginecrypto.DiffieHellman, []byte, error) {
	if group == 0 {
		_, err := childRekeySharedSecret(payloads, nil)
		return nil, nil, err
	}
	dh, err := enginecrypto.NewDiffieHellman(group)
	if err != nil {
		return nil, nil, err
	}
	if err := dh.GenerateKey(); err != nil {
		return nil, nil, err
	}
	secret, err := childRekeySharedSecret(payloads, dh)
	if err != nil {
		return nil, nil, err
	}
	return dh, secret, nil
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
	header := packetIKEHeader(request)
	return s.sendEncryptedResponseWithMsgID(payloads, header.ExchangeType, header.MessageID)
}

func (s *Session) handlePeerInformational(packet *ikev2.IKEPacket) error {
	header := packetIKEHeader(packet)
	context, retiredIKE, err := s.ikeContextForHeader(header)
	if err != nil {
		return err
	}
	if err := validateIKEContextRole(context, header); err != nil {
		return err
	}
	payloads, err := s.decryptAndParseWithKeys(packet, context.keys)
	if err != nil {
		return err
	}
	redirectAddress := s.runtimeRedirectAddress(payloads)
	responsePayloads, err := s.peerMOBIKEResponse(payloads, retiredIKE)
	if err != nil {
		return err
	}
	var activeChildDelete, ikeDelete bool
	var responseSPIs []uint32
	for _, payload := range payloads {
		deletion, ok := payload.(*ikev2.EncryptedPayloadDelete)
		if !ok {
			continue
		}
		if deletion.ProtocolID == ikev2.ProtoIKE {
			ikeDelete = true
			continue
		}
		if deletion.ProtocolID != ikev2.ProtoESP {
			continue
		}
		activeChildDelete = activeChildDelete || s.deleteContainsCurrentChildSA(deletion.SPIs)
		responseSPIs = append(responseSPIs, s.retireDeletedChildSAs(deletion.SPIs)...)
	}
	responsePayloads = append(responsePayloads, childSADeleteResponse(responseSPIs)...)
	var retiredResponse []byte
	if retiredIKE {
		retiredResponse, err = s.sendIKEContextResponse(packet, responsePayloads, context)
	} else {
		err = s.sendEstablishedIKEResponse(packet, responsePayloads)
	}
	if err != nil {
		return err
	}
	s.handleRuntimeRedirect(redirectAddress)
	if ikeDelete {
		if retiredIKE {
			return s.completeRetiredIKESADelete(context, packet, retiredResponse)
		}
		return errors.New("swu: peer deleted the active IKE_SA")
	}
	if activeChildDelete {
		return errors.New("swu: peer deleted the active CHILD_SA")
	}
	return nil
}

func childSADeleteResponse(spis []uint32) []ikev2.Payload {
	if len(spis) == 0 {
		return nil
	}
	responses := make([]ikev2.Payload, 0, (len(spis)+maxDeleteSPIs-1)/maxDeleteSPIs)
	for start := 0; start < len(spis); start += maxDeleteSPIs {
		end := min(start+maxDeleteSPIs, len(spis))
		encoded := make([]byte, 0, 4*(end-start))
		for _, spi := range spis[start:end] {
			encoded = append(encoded, spiBytes(spi)...)
		}
		responses = append(responses, &ikev2.EncryptedPayloadDelete{
			ProtocolID: ikev2.ProtoESP, SPISize: 4,
			NumSPIs: uint16(end - start), SPIs: encoded,
		})
	}
	return responses
}

func (s *Session) retireDeletedChildSAs(spis []byte) []uint32 {
	var retired []*ipsec.SecurityAssociation
	var retiredLocalSPIs []uint32
	var responseSPIs []uint32
	s.childSAMu.Lock()
	for len(spis) >= 4 {
		localSPI := binary.BigEndian.Uint32(spis[:4])
		if remoteSPI, ok := s.retiredChildSAs[localSPI]; ok {
			responseSPIs = append(responseSPIs, remoteSPI)
			retired = append(retired, s.espInboundSAs[localSPI])
			retiredLocalSPIs = append(retiredLocalSPIs, localSPI)
			delete(s.espInboundSAs, localSPI)
			delete(s.retiredChildSAs, localSPI)
		} else if localSPI == s.espLocalSPI {
			responseSPIs = append(responseSPIs, s.espRemoteSPI)
		}
		spis = spis[4:]
	}
	s.childSAMu.Unlock()
	for _, association := range retired {
		wipeESPAssociation(association)
	}
	for _, localSPI := range retiredLocalSPIs {
		s.retireKernelInboundChildSA(localSPI)
	}
	return responseSPIs
}

func (s *Session) deleteContainsCurrentChildSA(spis []byte) bool {
	s.childSAMu.RLock()
	current := s.espLocalSPI
	s.childSAMu.RUnlock()
	for len(spis) >= 4 {
		if binary.BigEndian.Uint32(spis[:4]) == current {
			return true
		}
		spis = spis[4:]
	}
	return false
}
