package ikev2

import (
	"encoding/binary"
	"errors"
	"fmt"
)

const maxPayloadLength = int(^uint16(0))

type IKEPacket struct {
	Header   *IKEHeader
	Payloads []Payload

	// These fields preserve the interim reconstructed API. Header is the
	// canonical representation whenever it is non-nil.
	InitiatorSPI [8]byte
	ResponderSPI [8]byte
	NextPayload  PayloadType
	Version      uint8
	ExchangeType ExchangeType
	Flags        uint8
	MessageID    uint32
	Length       uint32
}

func NewIKEPacket() *IKEPacket {
	return &IKEPacket{Header: &IKEHeader{}, Payloads: []Payload{}}
}

func (p *IKEPacket) Encode() ([]byte, error) {
	if p == nil {
		return nil, errors.New("IKE packet is nil")
	}
	header := p.effectiveHeader()
	payloadData, err := encodePayloads(p.Payloads)
	if err != nil {
		return nil, err
	}
	if len(p.Payloads) == 0 {
		header.NextPayload = NoNextPayload
	} else {
		header.NextPayload = p.Payloads[0].Type()
	}
	if header.Version == 0 {
		header.Version = 0x20
	}
	header.Length = uint32(IKE_HEADER_LEN + len(payloadData))
	p.Header = header
	p.syncCompatibilityFields()
	return append(header.Encode(), payloadData...), nil
}

func DecodePacket(data []byte) (*IKEPacket, error) {
	header, err := DecodeHeader(data)
	if err != nil {
		return nil, err
	}
	if header.Length < IKE_HEADER_LEN {
		return nil, fmt.Errorf("IKE 数据包长度非法: %d", header.Length)
	}
	if uint64(header.Length) > uint64(len(data)) {
		return nil, fmt.Errorf("IKE 数据包长度 %d 超过缓冲区 %d", header.Length, len(data))
	}
	packet := &IKEPacket{Header: header, Payloads: []Payload{}}
	packet.syncCompatibilityFields()
	payloads, err := decodePayloads(data[IKE_HEADER_LEN:header.Length], header.NextPayload)
	if err != nil {
		return nil, err
	}
	packet.Payloads = payloads
	return packet, nil
}

func EncodePayloadChainChecked(payloads []Payload) ([]byte, error) {
	return encodePayloads(payloads)
}

// EncodePayloadChain preserves the interim convenience API. Production paths
// should use EncodePayloadChainChecked so malformed payloads return an error.
func EncodePayloadChain(payloads []Payload) []byte {
	data, err := EncodePayloadChainChecked(payloads)
	if err != nil {
		panic(err)
	}
	return data
}

func DecodePayloadChain(data []byte) ([]Payload, error) {
	if len(data) == 0 {
		return nil, nil
	}
	return nil, errors.New("首个载荷类型不在加密载荷链中，请使用 DecodePayloadChainWithFirst")
}

func DecodePayloadChainWithFirst(first PayloadType, data []byte) ([]Payload, error) {
	return decodePayloads(data, first)
}

func encodePayloads(payloads []Payload) ([]byte, error) {
	var encoded []byte
	for index, item := range payloads {
		if item == nil {
			return nil, fmt.Errorf("IKE 载荷 %d 为 nil", index)
		}
		body, err := item.Encode()
		if err != nil {
			return nil, err
		}
		if len(body)+PAYLOAD_HEADER_LEN > maxPayloadLength {
			return nil, fmt.Errorf("IKE 载荷 %d 长度超过 uint16", index)
		}
		next := NoNextPayload
		if index+1 < len(payloads) {
			next = payloads[index+1].Type()
		}
		if override, ok := item.(nextPayloadOverride); ok {
			next = override.payloadNext()
		}
		header := PayloadHeader{NextPayload: next, PayloadLength: uint16(len(body) + PAYLOAD_HEADER_LEN)}
		encoded = append(encoded, header.Encode()...)
		encoded = append(encoded, body...)
	}
	return encoded, nil
}

func decodePayloads(data []byte, next PayloadType) ([]Payload, error) {
	var payloads []Payload
	for next != NoNextPayload {
		current := next
		if len(data) < PAYLOAD_HEADER_LEN {
			return nil, errors.New("数据包太短，无法包含载荷头部")
		}
		header, err := DecodePayloadHeader(data)
		if err != nil {
			return nil, err
		}
		length := int(header.PayloadLength)
		if length < PAYLOAD_HEADER_LEN || length > len(data) {
			return nil, errors.New("数据包太短，无法包含载荷主体")
		}
		payload, err := decodePayloadBody(next, header, data[PAYLOAD_HEADER_LEN:length])
		if err != nil {
			return nil, fmt.Errorf("解码载荷类型 %d 失败: %w", next, err)
		}
		payloads = append(payloads, payload)
		data = data[length:]
		if current == SK {
			if len(data) != 0 {
				return nil, fmt.Errorf("SK 载荷后包含 %d 个尾随字节", len(data))
			}
			return payloads, nil
		}
		next = header.NextPayload
	}
	if len(data) != 0 {
		return nil, fmt.Errorf("IKE 载荷链包含 %d 个尾随字节", len(data))
	}
	return payloads, nil
}

func (p *IKEPacket) effectiveHeader() *IKEHeader {
	if p.Header != nil {
		return p.Header
	}
	return &IKEHeader{
		SPIi: binary.BigEndian.Uint64(p.InitiatorSPI[:]), SPIr: binary.BigEndian.Uint64(p.ResponderSPI[:]),
		NextPayload: p.NextPayload, Version: p.Version, ExchangeType: p.ExchangeType,
		Flags: p.Flags, MessageID: p.MessageID, Length: p.Length,
	}
}

func (p *IKEPacket) syncCompatibilityFields() {
	binary.BigEndian.PutUint64(p.InitiatorSPI[:], p.Header.SPIi)
	binary.BigEndian.PutUint64(p.ResponderSPI[:], p.Header.SPIr)
	p.NextPayload = p.Header.NextPayload
	p.Version = p.Header.Version
	p.ExchangeType = p.Header.ExchangeType
	p.Flags = p.Header.Flags
	p.MessageID = p.Header.MessageID
	p.Length = p.Header.Length
}
