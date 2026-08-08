package ikev2

import (
	"encoding/binary"
	"errors"
	"fmt"
)

const IKE_HEADER_LEN = 28

type IKEHeader struct {
	SPIi         uint64
	SPIr         uint64
	NextPayload  PayloadType
	Version      uint8
	ExchangeType ExchangeType
	Flags        uint8
	MessageID    uint32
	Length       uint32
}

const (
	FlagInitiator uint8 = 1 << 3
	FlagVersion   uint8 = 1 << 4
	FlagResponse  uint8 = 1 << 5
)

func (h *IKEHeader) Encode() []byte {
	buf := make([]byte, IKE_HEADER_LEN)
	binary.BigEndian.PutUint64(buf[0:8], h.SPIi)
	binary.BigEndian.PutUint64(buf[8:16], h.SPIr)
	buf[16] = uint8(h.NextPayload)
	buf[17] = h.Version
	buf[18] = uint8(h.ExchangeType)
	buf[19] = h.Flags
	binary.BigEndian.PutUint32(buf[20:24], h.MessageID)
	binary.BigEndian.PutUint32(buf[24:28], h.Length)
	return buf
}

func DecodeHeader(data []byte) (*IKEHeader, error) {
	if len(data) < IKE_HEADER_LEN {
		return nil, errors.New("数据包太短，无法包含 IKE 头部")
	}
	return &IKEHeader{
		SPIi:         binary.BigEndian.Uint64(data[0:8]),
		SPIr:         binary.BigEndian.Uint64(data[8:16]),
		NextPayload:  PayloadType(data[16]),
		Version:      data[17],
		ExchangeType: ExchangeType(data[18]),
		Flags:        data[19],
		MessageID:    binary.BigEndian.Uint32(data[20:24]),
		Length:       binary.BigEndian.Uint32(data[24:28]),
	}, nil
}

func (h *IKEHeader) String() string {
	return fmt.Sprintf(
		"IKE Header: SPIi=%x SPIr=%x Next=%d Ver=%x Exch=%d Flags=%b MsgID=%d Len=%d",
		h.SPIi, h.SPIr, h.NextPayload, h.Version, h.ExchangeType, h.Flags, h.MessageID, h.Length,
	)
}
