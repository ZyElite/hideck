package swu

import (
	"encoding/binary"

	"github.com/iniwex5/vowifi-go/engine/ikev2"
)

func newIKEHeader(
	initiatorSPI, responderSPI [8]byte,
	exchangeType ikev2.ExchangeType,
	flags uint8,
	messageID uint32,
) *ikev2.IKEHeader {
	return &ikev2.IKEHeader{
		SPIi:    binary.BigEndian.Uint64(initiatorSPI[:]),
		SPIr:    binary.BigEndian.Uint64(responderSPI[:]),
		Version: 0x20, ExchangeType: exchangeType, Flags: flags, MessageID: messageID,
	}
}

func packetIKEHeader(packet *ikev2.IKEPacket) *ikev2.IKEHeader {
	if packet == nil {
		return nil
	}
	if packet.Header != nil {
		return packet.Header
	}
	return newIKEHeader(
		packet.InitiatorSPI, packet.ResponderSPI, packet.ExchangeType, packet.Flags, packet.MessageID,
	)
}

func ikeSPIBytes(spi uint64) [8]byte {
	var encoded [8]byte
	binary.BigEndian.PutUint64(encoded[:], spi)
	return encoded
}
