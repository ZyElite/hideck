package swu

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"

	"github.com/iniwex5/vowifi-go/engine/crypto"
	"github.com/iniwex5/vowifi-go/engine/ikev2"
)

func (s *Session) handleIKESAInitResp(data []byte) error {
	packet, err := ikev2.DecodePacket(data)
	if err != nil {
		return fmt.Errorf("解码 SA_INIT 响应失败: %v", err)
	}
	return s.handleIKESAInitPacket(packet)
}

func (s *Session) handleIKESAInitPacket(response *ikev2.IKEPacket) error {
	if response == nil {
		return errors.New("nil IKE_SA_INIT response")
	}
	header := packetIKEHeader(response)
	if header.ExchangeType != ikev2.IKE_SA_INIT {
		return fmt.Errorf("意外的交换类型: %d", header.ExchangeType)
	}
	parsed, err := s.parseIKEInitResponsePayloads(response.Payloads)
	if err != nil {
		return err
	}
	selection, err := s.validateIKESAInitSelection(parsed.sa)
	if err != nil {
		return err
	}
	group, peerKey, err := parseKEPayload(parsed.ke)
	if err != nil {
		return fmt.Errorf("parse KEr: %w", err)
	}
	if group != selection.dh || group != s.dhGroup {
		return fmt.Errorf("KEr DH group %d does not match negotiated group %d", group, selection.dh)
	}
	if err := s.applySelectedIKEAlgorithms(selection); err != nil {
		return err
	}
	shared, err := s.dh.ComputeSharedSecret(peerKey)
	if err != nil {
		return fmt.Errorf("DH 计算失败: %v", err)
	}
	s.SPIr = ikeSPIBytes(header.SPIr)
	s.setNr(parsed.nonce)
	s.dhSharedSecret = shared
	if err := s.GenerateIKESAKeys(parsed.nonce); err != nil {
		return fmt.Errorf("derive IKE SA keys: %w", err)
	}
	s.sendCookie = false
	s.applyNATTraversal(parsed.natSource, parsed.natDestination)
	return nil
}

type ikeInitResponsePayloads struct {
	sa             *ikev2.EncryptedPayloadSA
	ke             ikev2.Payload
	nonce          []byte
	natSource      []byte
	natDestination []byte
}

func (s *Session) parseIKEInitResponsePayloads(payloads []ikev2.Payload) (ikeInitResponsePayloads, error) {
	result := ikeInitResponsePayloads{}
	for _, payload := range payloads {
		switch payload.Type() {
		case ikev2.PayloadSA:
			value, ok := payload.(*ikev2.EncryptedPayloadSA)
			if !ok || result.sa != nil {
				return result, errors.New("invalid or duplicate IKE_SA_INIT SA payload")
			}
			result.sa = value
		case ikev2.PayloadKE:
			if result.ke != nil {
				return result, errors.New("duplicate IKE_SA_INIT KE payload")
			}
			result.ke = payload
		case ikev2.PayloadNi:
			value, ok := payload.(*ikev2.EncryptedPayloadNonce)
			if !ok || len(value.NonceData) == 0 || len(result.nonce) != 0 {
				return result, errors.New("invalid or duplicate IKE_SA_INIT nonce payload")
			}
			result.nonce = append([]byte(nil), value.NonceData...)
		case ikev2.PayloadNotify:
			if err := s.applyIKEInitNotify(payload, &result); err != nil {
				return result, err
			}
		}
	}
	if result.sa == nil || result.ke == nil || len(result.nonce) == 0 {
		return result, errors.New("SA_INIT 响应中缺少强制性载荷")
	}
	return result, nil
}

func (s *Session) applyIKEInitNotify(payload ikev2.Payload, result *ikeInitResponsePayloads) error {
	notifyType, data, ok := parseNotifyPayload(payload)
	if !ok {
		return errors.New("invalid IKE_SA_INIT Notify payload")
	}
	switch notifyType {
	case notifyCookie:
		if err := s.handleCookie(data); err != nil {
			return err
		}
		return ErrCookieRequired
	case notifyInvalidKE:
		if len(data) < 2 {
			return errors.New("服务器拒绝 DH 群组 (INVALID_KE_PAYLOAD), 并未指定期望群组")
		}
		return &ErrInvalidKEGroup{PreferredGroup: binary.BigEndian.Uint16(data[:2])}
	case notifyRedirectedTo:
		address, err := ParseRedirectData(data)
		if err != nil {
			return fmt.Errorf("redirect: %w", err)
		}
		return &RedirectError{NewAddr: address}
	case notifyNATSource:
		result.natSource = append([]byte(nil), data...)
	case notifyNATDestination:
		result.natDestination = append([]byte(nil), data...)
	case notifyFragmentation:
		s.fragmentationSupported = true
	case uint16(ikev2.NO_PROPOSAL_CHOSEN):
		return &NegotiationError{
			Class:  ErrClassAlgorithmCapabilityMismatch,
			Reason: "服务器拒绝了提议 (NO_PROPOSAL_CHOSEN)", Retryable: true,
		}
	default:
		if notifyType < 16384 {
			return fmt.Errorf("服务器在 SA_INIT 阶段拒绝：%s (code=%d)",
				ikev2.NotifyTypeToString(notifyType), notifyType)
		}
	}
	return nil
}

func (s *Session) validateIKESAInitSelection(sa *ikev2.EncryptedPayloadSA) (selectedAlgorithms, error) {
	if sa == nil || len(sa.Proposals) != 1 {
		return selectedAlgorithms{}, errors.New("IKE_SA_INIT response missing one selected SA proposal")
	}
	proposal := sa.Proposals[0]
	if proposal == nil || proposal.ProtocolID != ikev2.ProtoIKE || len(proposal.SPI) != 0 {
		return selectedAlgorithms{}, errors.New("IKE_SA_INIT response selected an invalid IKE proposal")
	}
	selection, err := firstIKEAlgorithmSelection(proposal)
	if err != nil {
		return selectedAlgorithms{}, err
	}
	if !selectionOffered(selection, s.offeredIKEProposals) {
		return selectedAlgorithms{}, errors.New("IKE_SA_INIT response selected an unoffered transform set")
	}
	if !buildAlgorithmPlan(s.cfg).allowsEncryption(ikev2.AlgorithmType(selection.encryption)) {
		return selectedAlgorithms{}, &NegotiationError{
			Class:     ErrClassAlgorithmPolicyRejected,
			Reason:    fmt.Sprintf("selected_encr=%s 被策略拒绝", ikev2.EncrToString(selection.encryption)),
			Retryable: true,
		}
	}
	return selection, nil
}

func selectionOffered(selection selectedAlgorithms, proposals []*ikev2.Proposal) bool {
	for _, proposal := range proposals {
		if proposalContainsSelection(proposal, selection) {
			return true
		}
	}
	return false
}

func proposalContainsSelection(proposal *ikev2.Proposal, selection selectedAlgorithms) bool {
	wanted := map[ikev2.TransformType]uint16{
		ikev2.TransformTypeEncr: selection.encryption,
		ikev2.TransformTypePRF:  selection.prf,
		ikev2.TransformTypeDH:   selection.dh,
	}
	if selection.integrity != 0 {
		wanted[ikev2.TransformTypeInteg] = selection.integrity
	}
	matched := make(map[ikev2.TransformType]bool, len(wanted))
	for _, transform := range proposal.Transforms {
		if transform == nil || wanted[transform.Type] != uint16(transform.ID) {
			continue
		}
		if transform.Type != ikev2.TransformTypeEncr || encryptionKeyBits(transform) == selection.keyBits {
			matched[transform.Type] = true
		}
	}
	return len(matched) == len(wanted)
}

func (s *Session) applySelectedIKEAlgorithms(selection selectedAlgorithms) error {
	prf, err := crypto.GetPRF(selection.prf)
	if err != nil {
		return capabilityNegotiationError("PRF", selection.prf, err)
	}
	encryption, err := supportedEncryption(selection.encryption, selection.keyBits)
	if err != nil {
		return capabilityNegotiationError("Encr", selection.encryption, err)
	}
	integrity, err := crypto.GetIntegrityAlgorithm(selection.integrity)
	if err != nil {
		return capabilityNegotiationError("Integ", selection.integrity, err)
	}
	s.encrAlg, s.encKeyBits, s.encKeyLen = selection.encryption, selection.keyBits, encryption.keyLen
	s.prfAlg, s.prf = selection.prf, prf
	s.integAlg, s.integKeyLen = selection.integrity, integrity.KeySize()
	s.dhGroup, s.aead = selection.dh, encryption.aead
	return nil
}

func capabilityNegotiationError(kind string, algorithm uint16, cause error) error {
	return &NegotiationError{
		Class:     ErrClassAlgorithmCapabilityMismatch,
		Reason:    fmt.Sprintf("选择了不支持的 %s: %d: %v", kind, algorithm, cause),
		Retryable: true,
	}
}

func (s *Session) applyNATTraversal(sourceHash, destinationHash []byte) {
	if s.socket == nil || len(sourceHash) == 0 || len(destinationHash) == 0 {
		return
	}
	expectedSource := natDetectionHash(s.SPIi, s.SPIr, s.socket.LocalIP(), s.socket.LocalPort())
	expectedDestination := natDetectionHash(
		s.SPIi, s.SPIr, s.socket.RemoteIP(), uint16(s.socket.RemotePort()),
	)
	if !bytes.Equal(sourceHash, expectedSource) || !bytes.Equal(destinationHash, expectedDestination) {
		s.socket.SetRemotePort(4500)
		s.remotePort = 4500
	}
}

func parseKERaw(payload *ikev2.RawPayload) (uint16, []byte, error) {
	if len(payload.Data) < 4 {
		return 0, nil, errors.New("KE payload too short")
	}
	return binary.BigEndian.Uint16(payload.Data[:2]), payload.Data[4:], nil
}

func parseKEPayload(payload ikev2.Payload) (uint16, []byte, error) {
	switch value := payload.(type) {
	case *ikev2.EncryptedPayloadKE:
		return uint16(value.DHGroup), value.KEData, nil
	case *ikev2.RawPayload:
		return parseKERaw(value)
	default:
		return 0, nil, fmt.Errorf("unexpected KE payload type %T", payload)
	}
}

func parseNotifyRaw(payload *ikev2.RawPayload) (uint16, []byte) {
	if len(payload.Data) < 4 {
		return 0, nil
	}
	spiSize := int(payload.Data[1])
	notifyType := binary.BigEndian.Uint16(payload.Data[2:4])
	offset := 4 + spiSize
	if offset > len(payload.Data) {
		return notifyType, nil
	}
	return notifyType, payload.Data[offset:]
}

func parseNotifyPayload(payload ikev2.Payload) (uint16, []byte, bool) {
	switch value := payload.(type) {
	case *ikev2.EncryptedPayloadNotify:
		return value.NotifyType, value.NotifyData, true
	case *ikev2.RawPayload:
		notifyType, data := parseNotifyRaw(value)
		return notifyType, data, true
	default:
		return 0, nil, false
	}
}
