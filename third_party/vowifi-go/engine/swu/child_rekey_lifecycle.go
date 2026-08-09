package swu

import (
	"encoding/binary"
	"errors"
	"fmt"

	enginecrypto "github.com/iniwex5/vowifi-go/engine/crypto"
	"github.com/iniwex5/vowifi-go/engine/ikev2"
	"github.com/iniwex5/vowifi-go/engine/ipsec"
	"github.com/iniwex5/vowifi-go/engine/logger"
	"go.uber.org/zap"
)

type kernelInboundSARetirer interface {
	RetireInbound(spi uint32) error
}

type childSARuntime struct {
	outbound, inbound   *ipsec.SecurityAssociation
	localSPI, remoteSPI uint32
	ni, nr              []byte
	tsi, tsr            *ikev2.EncryptedPayloadTS
	outboundKeys        childDirectionKeys
	inboundKeys         childDirectionKeys
	dh                  *enginecrypto.DiffieHellman
	dhSecret            []byte
}

type childSARuntimeSpec struct {
	localSPI       uint32
	remoteSPI      uint32
	initiatorNonce []byte
	responderNonce []byte
	sharedSecret   []byte
	dh             *enginecrypto.DiffieHellman
	tsi            *ikev2.EncryptedPayloadTS
	tsr            *ikev2.EncryptedPayloadTS
	localInitiator bool
}

func (s *Session) prepareChildSARuntime(spec childSARuntimeSpec) (*childSARuntime, error) {
	keys, err := s.deriveChildSAKeysForPFS(
		spec.initiatorNonce, spec.responderNonce, spec.sharedSecret,
	)
	if err != nil {
		return nil, err
	}
	outboundKeys, inboundKeys := keys.initiator, keys.responder
	if !spec.localInitiator {
		outboundKeys, inboundKeys = keys.responder, keys.initiator
	}
	outbound, err := s.newESPAssociation(spec.remoteSPI, outboundKeys)
	if err != nil {
		wipeChildSAKeys(keys)
		return nil, err
	}
	inbound, err := s.newESPAssociation(spec.localSPI, inboundKeys)
	if err != nil {
		wipeESPAssociation(outbound)
		wipeChildSAKeys(keys)
		return nil, err
	}
	return &childSARuntime{
		outbound: outbound, inbound: inbound,
		localSPI: spec.localSPI, remoteSPI: spec.remoteSPI,
		ni: append([]byte(nil), spec.initiatorNonce...), nr: append([]byte(nil), spec.responderNonce...),
		tsi: cloneTrafficSelectorPayload(spec.tsi), tsr: cloneTrafficSelectorPayload(spec.tsr),
		outboundKeys: outboundKeys, inboundKeys: inboundKeys,
		dh: spec.dh, dhSecret: append([]byte(nil), spec.sharedSecret...),
	}, nil
}

func (s *Session) activateChildSARuntime(runtime *childSARuntime) error {
	if runtime == nil {
		return errors.New("swu: nil CHILD_SA runtime")
	}
	if plane := s.currentKernelDataPlane(); plane != nil {
		if err := plane.Rekey(s, runtime); err != nil {
			wipeChildSARuntime(runtime)
			return fmt.Errorf("swu: rekey kernel CHILD_SA: %w", err)
		}
	}
	s.installChildSARuntime(runtime)
	return nil
}

func (s *Session) installChildSARuntime(runtime *childSARuntime) {
	s.childSAMu.Lock()
	oldOutbound, oldDH := s.espOutboundSA, s.childDH
	oldDHSecret := s.childDHSecret
	if s.espInboundSAs == nil {
		s.espInboundSAs = make(map[uint32]*ipsec.SecurityAssociation)
	}
	s.espOutboundSA, s.espInboundSA = runtime.outbound, runtime.inbound
	s.espInboundSAs[runtime.localSPI] = runtime.inbound
	s.espLocalSPI, s.espRemoteSPI = runtime.localSPI, runtime.remoteSPI
	s.childNi, s.childNr = append([]byte(nil), runtime.ni...), append([]byte(nil), runtime.nr...)
	s.childDH, s.childDHSecret = runtime.dh, append([]byte(nil), runtime.dhSecret...)
	s.childTSi, s.childTSr = cloneTrafficSelectorPayload(runtime.tsi), cloneTrafficSelectorPayload(runtime.tsr)
	enginecrypto.Wipe(s.espKey)
	enginecrypto.Wipe(s.espIntegKey)
	s.espKey = append([]byte(nil), runtime.outboundKeys.enc...)
	s.espIntegKey = append([]byte(nil), runtime.outboundKeys.integ...)
	s.syncLegacyChildStateLocked()
	wipeESPAssociation(oldOutbound)
	enginecrypto.Wipe(oldDHSecret)
	if oldDH != nil && oldDH != runtime.dh {
		enginecrypto.Wipe(oldDH.SharedKey)
	}
	s.childSAMu.Unlock()
	s.logChildKeys(childKeysFromRuntime(runtime))
}

func (s *Session) deleteOldChildSA(remoteSPI, localSPI uint32) error {
	if remoteSPI == 0 || localSPI == 0 {
		return errors.New("swu: old CHILD_SA SPIs must be non-zero")
	}
	response, err := s.sendEncryptedWithRetry([]ikev2.Payload{&ikev2.EncryptedPayloadDelete{
		ProtocolID: ikev2.ProtoESP, SPISize: 4, NumSPIs: 1, SPIs: spiBytes(remoteSPI),
	}}, ikev2.INFORMATIONAL)
	if err != nil {
		return err
	}
	packet, err := ikev2.DecodePacket(response)
	if err != nil {
		return fmt.Errorf("swu: decode CHILD_SA delete response: %w", err)
	}
	payloads, err := s.decryptAndParse(packet)
	if err != nil {
		return err
	}
	return validateChildSADeleteResponse(payloads, localSPI)
}

func validateChildSADeleteResponse(payloads []ikev2.Payload, expectedSPI uint32) error {
	if err := childSARejectionError(payloads); err != nil {
		return err
	}
	if len(payloads) == 0 {
		return nil
	}
	for _, payload := range payloads {
		deletion, ok := payload.(*ikev2.EncryptedPayloadDelete)
		if !ok || deletion.ProtocolID != ikev2.ProtoESP || deletion.SPISize != 4 {
			return fmt.Errorf("swu: invalid CHILD_SA delete response payloads %s", ikePayloadTypes(payloads))
		}
		for spis := deletion.SPIs; len(spis) >= 4; spis = spis[4:] {
			if binary.BigEndian.Uint32(spis[:4]) == expectedSPI {
				return nil
			}
		}
	}
	return fmt.Errorf("swu: CHILD_SA delete response does not acknowledge SPI %08x", expectedSPI)
}

func (s *Session) recordRetiredChildSA(remoteSPI, localSPI uint32) {
	s.childSAMu.Lock()
	defer s.childSAMu.Unlock()
	if s.retiredChildSAs == nil {
		s.retiredChildSAs = make(map[uint32]uint32)
	}
	s.retiredChildSAs[localSPI] = remoteSPI
}

func (s *Session) retireInboundChildSA(localSPI uint32) {
	s.childSAMu.Lock()
	retired := s.espInboundSAs[localSPI]
	delete(s.espInboundSAs, localSPI)
	delete(s.retiredChildSAs, localSPI)
	s.childSAMu.Unlock()
	wipeESPAssociation(retired)
	s.retireKernelInboundChildSA(localSPI)
}

func (s *Session) retireKernelInboundChildSA(localSPI uint32) {
	retirer, ok := s.currentKernelDataPlane().(kernelInboundSARetirer)
	if !ok {
		return
	}
	if err := retirer.RetireInbound(localSPI); err != nil {
		logger.Warn("retired XFRM inbound CHILD_SA cleanup failed",
			zap.Uint32("spi", localSPI), zap.Error(err))
	}
}

func wipeESPAssociation(sa *ipsec.SecurityAssociation) {
	if sa == nil {
		return
	}
	enginecrypto.Wipe(sa.EncryptionKey)
	enginecrypto.Wipe(sa.IntegrityKey)
	enginecrypto.Wipe(sa.IntegritySalt)
}

func wipeChildSAKeys(keys *childSAKeys) {
	if keys == nil {
		return
	}
	enginecrypto.Wipe(keys.initiator.enc)
	enginecrypto.Wipe(keys.initiator.integ)
	enginecrypto.Wipe(keys.responder.enc)
	enginecrypto.Wipe(keys.responder.integ)
}

func wipeChildSARuntime(runtime *childSARuntime) {
	if runtime == nil {
		return
	}
	wipeESPAssociation(runtime.outbound)
	wipeESPAssociation(runtime.inbound)
	enginecrypto.Wipe(runtime.outboundKeys.enc)
	enginecrypto.Wipe(runtime.outboundKeys.integ)
	enginecrypto.Wipe(runtime.inboundKeys.enc)
	enginecrypto.Wipe(runtime.inboundKeys.integ)
	enginecrypto.Wipe(runtime.dhSecret)
	if runtime.dh != nil {
		enginecrypto.Wipe(runtime.dh.SharedKey)
	}
}
