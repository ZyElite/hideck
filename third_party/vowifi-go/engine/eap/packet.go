package eap

import (
	"encoding/binary"
	"errors"
)

// Parse accepts the original one-argument form. The optional capacity
// argument preserves compatibility with the later reconstruction.
func Parse(data []byte, _ ...int) (*EAPPacket, error) {
	if len(data) < 4 {
		return nil, errors.New("EAP packet too short")
	}
	packet := &EAPPacket{Code: data[0], Identifier: data[1]}
	length := binary.BigEndian.Uint16(data[2:4])
	if int(length) > len(data) {
		return nil, errors.New("EAP length exceeds data")
	}
	if (packet.Code != CodeRequest && packet.Code != CodeResponse) || length <= 4 {
		return packet, nil
	}
	packet.Type = data[4]
	if packet.Type != TypeAKA && packet.Type != TypeAKAPrime {
		packet.Data = data[5:length]
		return packet, nil
	}
	if length > 5 {
		packet.Subtype = data[5]
		if length > 8 {
			packet.Data = data[8:length]
		}
	}
	return packet, nil
}

func (p *EAPPacket) Encode() []byte {
	length := 4
	if p.Code == CodeRequest || p.Code == CodeResponse {
		length = 5 + len(p.Data)
		if p.Type == TypeAKA || p.Type == TypeAKAPrime {
			length = 8 + len(p.Data)
		}
	}
	result := make([]byte, length)
	result[0] = p.Code
	result[1] = p.Identifier
	binary.BigEndian.PutUint16(result[2:4], uint16(length))
	if p.Code != CodeRequest && p.Code != CodeResponse {
		return result
	}
	result[4] = p.Type
	if p.Type == TypeAKA || p.Type == TypeAKAPrime {
		result[5] = p.Subtype
		copy(result[8:], p.Data)
		return result
	}
	copy(result[5:], p.Data)
	return result
}

// ParseAttributes accepts the original one-argument form and the later
// capacity argument. Values are zero-copy views into data, as in v1.5.5.
func ParseAttributes(data []byte, _ ...int) (map[uint8]*Attribute, error) {
	attributes := make(map[uint8]*Attribute)
	for offset := 0; offset < len(data); {
		if offset+2 > len(data) {
			break
		}
		attributeType := data[offset]
		attributeLength := int(data[offset+1]) * 4
		if attributeLength == 0 {
			return nil, errors.New("attribute length zero")
		}
		end := offset + attributeLength
		if end > len(data) {
			return nil, errors.New("attribute length exceeds data")
		}
		attributes[attributeType] = &Attribute{
			Type: attributeType, Length: data[offset+1], Value: data[offset+2 : end],
		}
		offset = end
	}
	return attributes, nil
}
