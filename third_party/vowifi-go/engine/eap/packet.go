package eap

import (
	"encoding/binary"
	"errors"
)

// Parse decodes an EAP packet from b (RFC 3748 §4). The slice length is n; cap
// is origCap (preserved on the returned Data slice header).
func Parse(b []byte, origCap int) (*EAPPacket, error) {
	if len(b) < 4 {
		return nil, errors.New("eap: packet too short")
	}
	p := &EAPPacket{Code: b[0], Identifier: b[1]}
	length := int(binary.BigEndian.Uint16(b[2:4]))
	if length > len(b) {
		return nil, errors.New("eap: packet length exceeds buffer")
	}

	if (p.Code == CodeRequest || p.Code == CodeResponse) && length > 4 {
		p.Type = b[4]
		if p.Type == TypeAKA || p.Type == TypeAKAPrime {
			// 8-byte AKA header: Code|ID|Len|Type|SubType|2 reserved.
			if length > 5 {
				p.SubType = b[5]
			}
			if length > 8 {
				p.Data = b[8:length]
			}
		} else {
			// 5-byte header: Code|ID|Len|Type.
			p.Data = b[5:length]
		}
	}
	return p, nil
}

// Encode serialises the EAP packet (RFC 3748 §4).
func (p *EAPPacket) Encode() []byte {
	var total int
	if p.Code == CodeRequest || p.Code == CodeResponse {
		if p.Type == TypeAKA || p.Type == TypeAKAPrime {
			total = len(p.Data) + 8
		} else {
			total = len(p.Data) + 5
		}
	} else {
		total = 4
	}

	out := make([]byte, total)
	out[0] = p.Code
	out[1] = p.Identifier
	binary.BigEndian.PutUint16(out[2:4], uint16(total))
	if p.Code == CodeRequest || p.Code == CodeResponse {
		out[4] = p.Type
		if p.Type == TypeAKA || p.Type == TypeAKAPrime {
			out[5] = p.SubType
			// out[6], out[7] reserved = 0
			copy(out[8:], p.Data)
		} else {
			copy(out[5:], p.Data)
		}
	}
	return out
}