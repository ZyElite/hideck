package swu

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"

	enginecrypto "github.com/iniwex5/vowifi-go/engine/crypto"
	"github.com/iniwex5/vowifi-go/engine/ikev2"
	"github.com/iniwex5/vowifi-go/engine/logger"
	"go.uber.org/zap"
)

type initiatedChildRekey struct {
	payloads     []ikev2.Payload
	nonce        []byte
	localSPI     uint32
	oldLocalSPI  uint32
	oldRemoteSPI uint32
	newDH        *enginecrypto.DiffieHellman
}

type childSARekeyRequest struct {
	localSPI uint32
	nonce    []byte
	tsi      *ikev2.EncryptedPayloadTS
	tsr      *ikev2.EncryptedPayloadTS
	dh       *enginecrypto.DiffieHellman
}

func (s *Session) performChildSARekey(ctx context.Context) error {
	s.ikeExchangeMu.Lock()
	defer s.ikeExchangeMu.Unlock()
	if s.transport() == nil || s.ikeKeys == nil || s.State() != stateEstablished {
		return errors.New("swu: session not established")
	}
	ni, localSPI, err := s.newChildSAInitiatorMaterial()
	if err != nil {
		return err
	}
	tsi, tsr := s.currentChildSelectors()
	newDH, err := s.newChildSARekeyDH()
	if err != nil {
		return err
	}
	payloads := s.buildChildSARekeyPayloads(childSARekeyRequest{
		localSPI: localSPI, nonce: ni, tsi: tsi, tsr: tsr, dh: newDH,
	})
	response, err := s.sendEncryptedWithRetry(payloads, ikev2.CREATE_CHILD_SA)
	if err != nil {
		return err
	}
	return s.handleCreateChildSAResp(response, ni, localSPI, newDH)
}

func (s *Session) completeInitiatedChildSARekey(
	payloads []ikev2.Payload,
	initiatorNonce []byte,
	localSPI uint32,
) error {
	s.childSAMu.RLock()
	oldLocalSPI, oldRemoteSPI := s.espLocalSPI, s.espRemoteSPI
	s.childSAMu.RUnlock()
	return s.completeInitiatedChildSARekeyWithOld(initiatedChildRekey{
		payloads: payloads, nonce: initiatorNonce, localSPI: localSPI,
		oldLocalSPI: oldLocalSPI, oldRemoteSPI: oldRemoteSPI,
	})
}

func (s *Session) completeInitiatedChildSARekeyWithOld(rekey initiatedChildRekey) error {
	installed := false
	defer func() {
		if !installed && rekey.newDH != nil {
			enginecrypto.Wipe(rekey.newDH.SharedKey)
		}
	}()
	tsi, tsr := s.currentChildSelectors()
	selection, err := validateChildSAResponse(rekey.payloads, childSAOffer{
		encryption: s.espCipher, encryptionKeyBits: s.espEncKeyBits, integrity: s.espInteg,
		dhGroup: childDHGroup(rekey.newDH), tsi: tsi, tsr: tsr, localIPs: configuredInnerIPs(s),
		requireSA: true, requireNonce: true,
	})
	if err != nil {
		return err
	}
	sharedSecret, err := childRekeySharedSecret(rekey.payloads, rekey.newDH)
	if err != nil {
		return err
	}
	runtime, err := s.prepareChildSARuntime(
		childSARuntimeSpec{
			localSPI: rekey.localSPI, remoteSPI: selection.remoteSPI,
			initiatorNonce: rekey.nonce, responderNonce: selection.nonce,
			sharedSecret: sharedSecret, tsi: selection.tsi, tsr: selection.tsr,
			dh: rekey.newDH, localInitiator: true,
		},
	)
	if err != nil {
		return err
	}
	if err := s.activateChildSARuntime(runtime); err != nil {
		return err
	}
	if err := s.deleteOldChildSA(rekey.oldRemoteSPI, rekey.oldLocalSPI); err != nil {
		logger.Warn("CHILD_SA rekey switched but old SA delete failed", zap.Error(err))
		s.recordRetiredChildSA(rekey.oldRemoteSPI, rekey.oldLocalSPI)
	} else {
		s.retireInboundChildSA(rekey.oldLocalSPI)
	}
	s.markChildRekeyComplete()
	installed = true
	return nil
}

func childRekeySharedSecret(
	payloads []ikev2.Payload,
	dh *enginecrypto.DiffieHellman,
) ([]byte, error) {
	var sharedSecret []byte
	for _, payload := range payloads {
		if payload == nil || payload.Type() != ikev2.PayloadKE {
			continue
		}
		if sharedSecret != nil {
			return nil, errors.New("swu: duplicate CHILD_SA rekey KE payload")
		}
		if dh == nil {
			return nil, errors.New("swu: CHILD_SA rekey contains unoffered KE payload")
		}
		group, peerKey, err := parseKEPayload(payload)
		if err != nil {
			return nil, err
		}
		if group != dh.Group {
			return nil, fmt.Errorf("swu: CHILD_SA rekey DH group %d does not match %d", group, dh.Group)
		}
		sharedSecret, err = dh.ComputeSharedSecret(peerKey)
		if err != nil {
			return nil, fmt.Errorf("swu: compute CHILD_SA rekey DH secret: %w", err)
		}
	}
	if dh != nil && sharedSecret == nil {
		return nil, errors.New("swu: PFS CHILD_SA rekey response missing KE payload")
	}
	return sharedSecret, nil
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

func (s *Session) newChildSARekeyDH() (*enginecrypto.DiffieHellman, error) {
	group := s.currentChildDHGroup()
	if group == 0 {
		return nil, nil
	}
	dh, err := enginecrypto.NewDiffieHellman(group)
	if err != nil {
		return nil, fmt.Errorf("swu: create CHILD_SA rekey PFS group %d: %w", group, err)
	}
	if err := dh.GenerateKey(); err != nil {
		return nil, fmt.Errorf("swu: generate CHILD_SA rekey PFS key: %w", err)
	}
	return dh, nil
}

func (s *Session) currentChildDHGroup() uint16 {
	s.childSAMu.RLock()
	defer s.childSAMu.RUnlock()
	return childDHGroup(s.childDH)
}

func childDHGroup(dh *enginecrypto.DiffieHellman) uint16 {
	if dh == nil {
		return 0
	}
	return dh.Group
}

func (s *Session) buildChildSARekeyRequest(localSPI uint32, nonce []byte, tsi, tsr *ikev2.EncryptedPayloadTS) *ikev2.IKEPacket {
	return &ikev2.IKEPacket{
		Header: newIKEHeader(
			s.SPIi, s.SPIr, ikev2.CREATE_CHILD_SA, s.localIKEFlags(false), s.nextMessageID(),
		),
		Payloads: s.buildChildSARekeyPayloads(childSARekeyRequest{
			localSPI: localSPI, nonce: nonce, tsi: tsi, tsr: tsr,
		}),
	}
}

func (s *Session) buildChildSARekeyPayloads(request childSARekeyRequest) []ikev2.Payload {
	s.childSAMu.RLock()
	oldRemoteSPI := s.espRemoteSPI
	s.childSAMu.RUnlock()
	proposals := buildESPProposalsForSession(s, request.localSPI)
	if request.dh != nil {
		proposals[0].AddTransform(ikev2.TransformTypeDH, ikev2.AlgorithmType(request.dh.Group), 0)
	}
	payloads := []ikev2.Payload{
		&ikev2.EncryptedPayloadSA{Proposals: proposals},
		&ikev2.EncryptedPayloadNonce{NonceData: append([]byte(nil), request.nonce...)},
	}
	if request.dh != nil {
		payloads = append(payloads, &ikev2.EncryptedPayloadKE{
			DHGroup: ikev2.AlgorithmType(request.dh.Group), KEData: request.dh.PublicKeyBytes(),
		})
	}
	return append(payloads,
		&ikev2.EncryptedPayloadNotify{
			ProtocolID: ikev2.ProtoESP,
			NotifyType: ikev2.NotifyTypeRekeySA, SPI: spiBytes(oldRemoteSPI),
		},
		cloneTrafficSelectorPayload(request.tsi),
		cloneTrafficSelectorPayload(request.tsr),
	)
}

func (s *Session) exchangeEstablishedIKE(ctx context.Context, packet *ikev2.IKEPacket) ([]ikev2.Payload, error) {
	raw, err := s.encryptAndWrap(packet)
	if err != nil {
		return nil, err
	}
	responseData, err := s.exchangeEstablishedRaw(ctx, packet, [][]byte{raw})
	if err != nil {
		return nil, err
	}
	response, err := ikev2.DecodePacket(responseData)
	if err != nil {
		return nil, fmt.Errorf("swu: decode established IKE response: %w", err)
	}
	if !validIKEResponseHeader(response, packet) {
		return nil, errors.New("swu: established IKE response header mismatch")
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
