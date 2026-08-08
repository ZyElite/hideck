package swu

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"

	enginecrypto "github.com/iniwex5/vowifi-go/engine/crypto"
	"github.com/iniwex5/vowifi-go/engine/ikev2"
	"github.com/iniwex5/vowifi-go/engine/logger"
	"go.uber.org/zap"
)

type ikeSARekeySelection struct {
	responderSPI [8]byte
	nonce        []byte
	peerKey      []byte
}

type initiatedIKERekey struct {
	ctx          context.Context
	payloads     []ikev2.Payload
	nonce        []byte
	dh           *enginecrypto.DiffieHellman
	initiatorSPI [8]byte
	oldSKd       []byte
	oldSPIi      [8]byte
	oldSPIr      [8]byte
	oldInitiator bool
}

func (s *Session) performIKESARekey(ctx context.Context) error {
	s.ikeExchangeMu.Lock()
	defer s.ikeExchangeMu.Unlock()
	if s.transport() == nil || s.ikeKeys == nil || s.State() != stateEstablished {
		return errors.New("swu: session not established")
	}
	dh, initiatorSPI, nonce, err := s.newIKESARekeyMaterial()
	if err != nil {
		return err
	}
	proposals := buildIKEProposalsForSession(s)
	if len(proposals) == 0 || proposals[0] == nil {
		return errors.New("swu: no IKE proposal available for rekey")
	}
	proposals[0].SPI = append([]byte(nil), initiatorSPI[:]...)
	payloads := []ikev2.Payload{
		&ikev2.EncryptedPayloadSA{Proposals: proposals},
		&ikev2.EncryptedPayloadNonce{NonceData: append([]byte(nil), nonce...)},
		&ikev2.EncryptedPayloadKE{DHGroup: ikev2.AlgorithmType(s.dhGroup), KEData: dh.PublicKeyBytes()},
	}
	s.mu.RLock()
	oldSPIi, oldSPIr := s.SPIi, s.SPIr
	oldSKd := append([]byte(nil), s.ikeKeys.SK_d...)
	s.mu.RUnlock()
	defer enginecrypto.Wipe(oldSKd)
	response, err := s.sendEncryptedWithRetry(payloads, ikev2.CREATE_CHILD_SA)
	if err != nil {
		return err
	}
	return s.handleRekeyIKESAResp(
		response, nonce, dh, binary.BigEndian.Uint64(initiatorSPI[:]), oldSKd,
		binary.BigEndian.Uint64(oldSPIi[:]), binary.BigEndian.Uint64(oldSPIr[:]),
	)
}

func (s *Session) completeInitiatedIKESARekey(rekey initiatedIKERekey) error {
	installed := false
	defer func() {
		if !installed && rekey.dh != nil {
			enginecrypto.Wipe(rekey.dh.SharedKey)
		}
	}()
	selection, err := s.validateIKESARekeyResponse(rekey.payloads)
	if err != nil {
		return err
	}
	sharedSecret, err := rekey.dh.ComputeSharedSecret(selection.peerKey)
	if err != nil {
		return fmt.Errorf("swu: compute IKE rekey DH secret: %w", err)
	}
	newKeys, err := s.GenerateIKESARekeyKeys(
		rekey.oldSKd, sharedSecret, rekey.nonce, selection.nonce,
		binary.BigEndian.Uint64(rekey.initiatorSPI[:]),
		binary.BigEndian.Uint64(selection.responderSPI[:]),
	)
	if err != nil {
		return fmt.Errorf("swu: derive rekeyed IKE SA keys: %w", err)
	}
	deleteErr := s.deleteOldIKESA(rekey.ctx)
	if deleteErr != nil {
		logger.Warn("IKE SA rekey switched but old SA delete failed", zap.Error(deleteErr))
	}
	s.mu.Lock()
	if s.SPIi != rekey.oldSPIi || s.SPIr != rekey.oldSPIr {
		s.mu.Unlock()
		wipeIKEKeys(newKeys)
		return errors.New("swu: active IKE SA changed during rekey")
	}
	oldKeys := s.ikeKeys
	oldDH := s.dh
	oldDHSecret := s.dhSharedSecret
	var displaced *ikeSAContext
	if deleteErr != nil {
		displaced = s.retiredIKESA
		s.retiredIKEDelete = nil
		s.retiredIKESA = &ikeSAContext{
			spiI: rekey.oldSPIi, spiR: rekey.oldSPIr,
			keys: oldKeys, localInitiator: rekey.oldInitiator,
		}
	}
	s.SPIi, s.SPIr = rekey.initiatorSPI, selection.responderSPI
	s.localIKEInitiator = true
	s.ikeKeys = newKeys
	s.dh = rekey.dh
	s.dhSharedSecret = append([]byte(nil), sharedSecret...)
	s.Ni = append([]byte(nil), rekey.nonce...)
	s.nr = append([]byte(nil), selection.nonce...)
	s.nextOutboundID = 0
	s.mu.Unlock()
	s.fragmentBuf.clear()
	if deleteErr == nil {
		wipeIKEKeys(oldKeys)
	}
	if displaced != nil && displaced.keys != oldKeys {
		wipeIKEKeys(displaced.keys)
	}
	enginecrypto.Wipe(oldDHSecret)
	if oldDH != nil && oldDH != rekey.dh {
		enginecrypto.Wipe(oldDH.SharedKey)
	}
	s.markIKERekeyComplete()
	installed = true
	return nil
}

func (s *Session) handlePeerIKESARekey(packet *ikev2.IKEPacket, payloads []ikev2.Payload) error {
	requestHeader := packetIKEHeader(packet)
	selection, err := s.validateIKESARekeyResponse(payloads)
	if err != nil {
		return err
	}
	dh, responderSPI, responderNonce, err := s.newIKESARekeyMaterial()
	if err != nil {
		return err
	}
	installed := false
	defer func() {
		if !installed {
			enginecrypto.Wipe(dh.SharedKey)
		}
	}()
	sharedSecret, err := dh.ComputeSharedSecret(selection.peerKey)
	if err != nil {
		return fmt.Errorf("swu: compute peer IKE rekey DH secret: %w", err)
	}
	s.mu.RLock()
	oldKeys := s.ikeKeys
	if oldKeys == nil || len(oldKeys.SK_d) == 0 {
		s.mu.RUnlock()
		return errors.New("swu: peer IKE SA rekey requires active key material")
	}
	oldSKd := append([]byte(nil), oldKeys.SK_d...)
	s.mu.RUnlock()
	defer enginecrypto.Wipe(oldSKd)
	newKeys, err := s.GenerateIKESARekeyKeys(
		oldSKd, sharedSecret, selection.nonce, responderNonce,
		binary.BigEndian.Uint64(selection.responderSPI[:]),
		binary.BigEndian.Uint64(responderSPI[:]),
	)
	if err != nil {
		return fmt.Errorf("swu: derive peer-rekeyed IKE SA keys: %w", err)
	}
	proposals := buildIKEProposalsForSession(s)
	if len(proposals) == 0 || proposals[0] == nil {
		wipeIKEKeys(newKeys)
		return errors.New("swu: no IKE proposal available for peer rekey")
	}
	proposals[0].SPI = append([]byte(nil), responderSPI[:]...)
	responsePayloads := []ikev2.Payload{
		&ikev2.EncryptedPayloadSA{Proposals: proposals},
		&ikev2.EncryptedPayloadNonce{NonceData: append([]byte(nil), responderNonce...)},
		&ikev2.EncryptedPayloadKE{DHGroup: ikev2.AlgorithmType(s.dhGroup), KEData: dh.PublicKeyBytes()},
	}
	if err := s.sendEstablishedIKEResponse(packet, responsePayloads); err != nil {
		wipeIKEKeys(newKeys)
		enginecrypto.Wipe(dh.SharedKey)
		return err
	}
	s.mu.Lock()
	if binary.BigEndian.Uint64(s.SPIi[:]) != requestHeader.SPIi ||
		binary.BigEndian.Uint64(s.SPIr[:]) != requestHeader.SPIr {
		s.mu.Unlock()
		wipeIKEKeys(newKeys)
		enginecrypto.Wipe(dh.SharedKey)
		return errors.New("swu: active IKE SA changed while answering peer rekey")
	}
	oldDH := s.dh
	oldDHSecret := s.dhSharedSecret
	oldContext := &ikeSAContext{
		spiI: s.SPIi, spiR: s.SPIr, keys: oldKeys,
		localInitiator: s.localIKEInitiator,
	}
	displaced := s.retiredIKESA
	s.retiredIKEDelete = nil
	s.retiredIKESA = oldContext
	s.SPIi, s.SPIr = selection.responderSPI, responderSPI
	s.localIKEInitiator = false
	s.ikeKeys = newKeys
	s.dh = dh
	s.dhSharedSecret = append([]byte(nil), sharedSecret...)
	s.Ni = append([]byte(nil), selection.nonce...)
	s.nr = append([]byte(nil), responderNonce...)
	s.nextOutboundID = 0
	s.mu.Unlock()
	s.fragmentBuf.clear()
	if displaced != nil && displaced.keys != oldKeys {
		wipeIKEKeys(displaced.keys)
	}
	enginecrypto.Wipe(oldDHSecret)
	if oldDH != nil && oldDH != dh {
		enginecrypto.Wipe(oldDH.SharedKey)
	}
	s.markIKERekeyComplete()
	installed = true
	return nil
}

func (s *Session) newIKESARekeyMaterial() (*enginecrypto.DiffieHellman, [8]byte, []byte, error) {
	dh, err := enginecrypto.NewDiffieHellman(s.dhGroup)
	if err != nil {
		return nil, [8]byte{}, nil, err
	}
	if err := dh.GenerateKey(); err != nil {
		return nil, [8]byte{}, nil, err
	}
	var spi [8]byte
	for attempts := 0; attempts < 3 && spi == ([8]byte{}); attempts++ {
		if _, err := rand.Read(spi[:]); err != nil {
			return nil, [8]byte{}, nil, err
		}
	}
	if spi == ([8]byte{}) {
		return nil, [8]byte{}, nil, errors.New("swu: generated zero IKE rekey SPI")
	}
	nonce := make([]byte, s.nonceLen)
	if _, err := rand.Read(nonce); err != nil {
		return nil, [8]byte{}, nil, err
	}
	return dh, spi, nonce, nil
}

func (s *Session) deleteOldIKESA(ctx context.Context) error {
	request := &ikev2.IKEPacket{
		Header: newIKEHeader(
			s.SPIi, s.SPIr, ikev2.INFORMATIONAL, s.localIKEFlags(false), s.nextMessageID(),
		),
		Payloads: []ikev2.Payload{&ikev2.EncryptedPayloadDelete{
			ProtocolID: ikev2.ProtoIKE,
		}},
	}
	payloads, err := s.exchangeEstablishedIKE(ctx, request)
	if err != nil {
		return err
	}
	if len(payloads) != 0 {
		return fmt.Errorf("swu: old IKE SA delete response contains payloads %s", ikePayloadTypes(payloads))
	}
	return nil
}
