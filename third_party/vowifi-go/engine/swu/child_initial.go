package swu

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"net"

	"github.com/iniwex5/vowifi-go/engine/crypto"
	"github.com/iniwex5/vowifi-go/engine/ikev2"
)

func (s *Session) createInitialChildSA(ctx context.Context) error {
	ni := make([]byte, s.nonceLen)
	if _, err := rand.Read(ni); err != nil {
		return err
	}
	localSPI, err := randomChildSPI()
	if err != nil {
		return err
	}
	espProposals := buildESPProposalsForSession(s, localSPI)
	childDH, err := s.newChildSARekeyDH()
	if err != nil {
		return err
	}
	if childDH != nil {
		espProposals[0].AddTransform(ikev2.TransformTypeDH, ikev2.AlgorithmType(childDH.Group), 0)
	}
	installed := false
	defer func() {
		if !installed && childDH != nil {
			crypto.Wipe(childDH.SharedKey)
		}
	}()
	tsi, tsr := buildTrafficSelectorsForIPStack(s.primaryInnerIP())
	payloads := []ikev2.Payload{
		&ikev2.EncryptedPayloadSA{Proposals: espProposals},
		&ikev2.EncryptedPayloadNonce{NonceData: ni},
	}
	if childDH != nil {
		payloads = append(payloads, &ikev2.EncryptedPayloadKE{
			DHGroup: ikev2.AlgorithmType(childDH.Group), KEData: childDH.PublicKeyBytes(),
		})
	}
	payloads = append(payloads, tsi, tsr)
	request := &ikev2.IKEPacket{
		Header: newIKEHeader(
			s.SPIi, s.SPIr, ikev2.CREATE_CHILD_SA, s.localIKEFlags(false), s.nextMessageID(),
		),
		Payloads: payloads,
	}
	raw, err := s.encryptAndWrap(request)
	if err != nil {
		return err
	}
	if err := s.sendIKE(raw); err != nil {
		return err
	}
	response, err := s.receiveIKE(ctx)
	if err != nil {
		return err
	}
	err = s.applyInitialChildSAResponse(initialChildSAExchange{
		response: response, localSPI: localSPI, initiatorNonce: ni,
		tsi: tsi, tsr: tsr, dh: childDH,
	})
	installed = err == nil
	return err
}

type initialChildSAExchange struct {
	response       *ikev2.IKEPacket
	localSPI       uint32
	initiatorNonce []byte
	tsi, tsr       *ikev2.EncryptedPayloadTS
	dh             *crypto.DiffieHellman
}

func (s *Session) applyInitialChildSAResponse(exchange initialChildSAExchange) error {
	payloads, err := s.decryptAndParse(exchange.response)
	if err != nil {
		return err
	}
	selection, err := validateChildSAResponse(payloads, childSAOffer{
		encryption: s.espCipher, encryptionKeyBits: s.espEncKeyBits, integrity: s.espInteg,
		dhGroup: childDHGroup(exchange.dh),
		tsi:     exchange.tsi, tsr: exchange.tsr, localIPs: configuredInnerIPs(s),
		requireSA: true, requireNonce: true,
	})
	if err != nil {
		return err
	}
	sharedSecret, err := childRekeySharedSecret(payloads, exchange.dh)
	if err != nil {
		return err
	}
	s.espLocalSPI, s.espRemoteSPI = exchange.localSPI, selection.remoteSPI
	s.espCipher, s.espInteg = selection.encryption, selection.integrity
	s.childNi = append([]byte(nil), exchange.initiatorNonce...)
	s.childNr = append([]byte(nil), selection.nonce...)
	s.childDH, s.childDHSecret = exchange.dh, append([]byte(nil), sharedSecret...)
	s.childTSi, s.childTSr = selection.tsi, selection.tsr
	return nil
}

func configuredInnerIPs(session *Session) []net.IP {
	var ips []net.IP
	if session.innerIP != nil {
		ips = append(ips, append(net.IP(nil), session.innerIP...))
	}
	if session.innerIPv6 != nil {
		ips = append(ips, append(net.IP(nil), session.innerIPv6...))
	}
	return ips
}

func randomChildSPI() (uint32, error) {
	var raw [4]byte
	for attempts := 0; attempts < 3; attempts++ {
		if _, err := rand.Read(raw[:]); err != nil {
			return 0, fmt.Errorf("generate CHILD_SA SPI: %w", err)
		}
		if spi := binary.BigEndian.Uint32(raw[:]); spi != 0 {
			return spi, nil
		}
	}
	return 0, errors.New("generate CHILD_SA SPI: random source returned zero")
}
