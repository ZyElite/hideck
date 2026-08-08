package swu

import (
	"errors"
	"fmt"

	"github.com/iniwex5/vowifi-go/engine/ikev2"
)

const maxDeleteSPIs = int(^uint16(0))

func (s *Session) sendDeleteChildSA(spis []uint32) error {
	if len(spis) == 0 {
		return nil
	}
	if len(spis) > maxDeleteSPIs {
		return fmt.Errorf("swu: too many CHILD_SA SPIs to delete: %d", len(spis))
	}
	if s.transport() == nil || s.ikeKeys == nil {
		return errors.New("swu: session not established")
	}
	encodedSPIs := make([]byte, 0, 4*len(spis))
	for _, spi := range spis {
		encodedSPIs = append(encodedSPIs, spiBytes(spi)...)
	}
	request := &ikev2.IKEPacket{
		Header: newIKEHeader(
			s.SPIi, s.SPIr, ikev2.INFORMATIONAL, s.localIKEFlags(false), s.nextMessageID(),
		),
		Payloads: []ikev2.Payload{&ikev2.EncryptedPayloadDelete{
			ProtocolID: ikev2.ProtoESP, SPISize: 4,
			NumSPIs: uint16(len(spis)), SPIs: encodedSPIs,
		}},
	}
	raw, err := s.encryptAndWrap(request)
	if err != nil {
		return err
	}
	return s.sendIKE(raw)
}

func (s *Session) sendDeleteIKE() error {
	if s.transport() == nil || s.ikeKeys == nil {
		return errors.New("swu: session not established")
	}
	request := &ikev2.IKEPacket{
		Header: newIKEHeader(
			s.SPIi, s.SPIr, ikev2.INFORMATIONAL, s.localIKEFlags(false), s.nextMessageID(),
		),
		Payloads: []ikev2.Payload{&ikev2.EncryptedPayloadDelete{ProtocolID: ikev2.ProtoIKE}},
	}
	raw, err := s.encryptAndWrap(request)
	if err != nil {
		return err
	}
	return s.sendIKE(raw)
}

func spiBytes(spi uint32) []byte {
	return []byte{byte(spi >> 24), byte(spi >> 16), byte(spi >> 8), byte(spi)}
}
