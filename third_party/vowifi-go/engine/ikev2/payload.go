package ikev2

import (
	"encoding/binary"
	"errors"
	"fmt"
)

const PAYLOAD_HEADER_LEN = 4

type Payload interface {
	Type() PayloadType
	Encode() ([]byte, error)
}

type PayloadHeader struct {
	NextPayload   PayloadType
	Critical      bool
	Reserved      uint8
	PayloadLength uint16
	Length        uint16
}

func (h *PayloadHeader) Encode() []byte {
	buf := make([]byte, PAYLOAD_HEADER_LEN)
	buf[0] = uint8(h.NextPayload)
	if h.Critical {
		buf[1] = 0x80
	}
	length := h.PayloadLength
	if length == 0 {
		length = h.Length
	}
	binary.BigEndian.PutUint16(buf[2:4], length)
	return buf
}

func DecodePayloadHeader(data []byte) (*PayloadHeader, error) {
	if len(data) < PAYLOAD_HEADER_LEN {
		return nil, errors.New("通用载荷头部太短")
	}
	length := binary.BigEndian.Uint16(data[2:4])
	return &PayloadHeader{
		NextPayload:   PayloadType(data[0]),
		Critical:      data[1]&0x80 != 0,
		Reserved:      data[1] & 0x7f,
		PayloadLength: length,
		Length:        length,
	}, nil
}

func errPayloadTooShort(name string) error {
	return fmt.Errorf("%s 载荷数据太短", name)
}
