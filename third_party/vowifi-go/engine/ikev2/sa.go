package ikev2

import (
	"encoding/binary"
	"fmt"
)

// Protocol IDs (RFC 7296 §3.3.2).
const (
	ProtoIKE byte = 1
	ProtoESP byte = 3
	ProtoAH  byte = 2
)

// Transform types (RFC 7296 §3.3.2).
const (
	TypeEncryption byte = 1
	TypePRF        byte = 2
	TypeIntegrity  byte = 3
	TypeDHGroup    byte = 4
	TypeESN        byte = 5
)

// Proposal is one proposal inside an SA payload.
type Proposal struct {
	ProposalNum   byte
	ProtocolID    byte
	SPISize       byte
	NumTransforms byte
	SPI           []byte
	Transforms    []*Transform
}

// AddTransform appends an encryption/integrity/etc. transform with the given
// transform ID and no key-length attribute.
func (p *Proposal) AddTransform(transformType byte, transformID uint16) {
	p.Transforms = append(p.Transforms, &Transform{
		TransformType: transformType,
		TransformID:   transformID,
	})
	p.NumTransforms = byte(len(p.Transforms))
}

// AddTransformWithKeyLen appends a transform with an explicit key-length
// attribute (used for AES-CBC/GCM).
func (p *Proposal) AddTransformWithKeyLen(transformType byte, transformID uint16, keyLen uint16) {
	p.Transforms = append(p.Transforms, &Transform{
		TransformType: transformType,
		TransformID:   transformID,
		Attributes:    []*TransformAttribute{{Type: 14, Value: uint16(keyLen)}}, // KEY_LENGTH = 14
	})
	p.NumTransforms = byte(len(p.Transforms))
}

// Encode serialises the proposal. Unlike transforms, a proposal has no
// length field — it runs until its transforms are exhausted.
func (p *Proposal) Encode(b []byte) []byte {
	// header: R(1bit)+ProposalNum(7bits) | ProtocolID | SPISize | NumTransforms
	body := []byte{p.ProposalNum & 0x7f, p.ProtocolID, p.SPISize, p.NumTransforms}
	body = append(body, p.SPI...)
	for _, t := range p.Transforms {
		body = t.Encode(body)
	}
	return append(b, body...)
}

// Transform is one transform inside a proposal.
type Transform struct {
	TransformType byte
	TransformID   uint16
	Attributes    []*TransformAttribute
}

// Encode serialises the transform (2-byte length prefix + body).
func (t *Transform) Encode(b []byte) []byte {
	body := []byte{t.TransformType, 0}
	body = binary.BigEndian.AppendUint16(body, t.TransformID)
	for _, a := range t.Attributes {
		body = a.Encode(body)
	}
	b = binary.BigEndian.AppendUint16(b, uint16(len(body)))
	return append(b, body...)
}

// TransformAttribute is a variable-length transform attribute.
type TransformAttribute struct {
	Type  uint16
	Value uint16
}

// Encode serialises the attribute (2-byte value form, RFC 7296 §3.3.5).
func (a *TransformAttribute) Encode(b []byte) []byte {
	// bit 15 = 1 => attribute value present in the next 2 bytes.
	word := a.Type&0x7fff | 0x8000
	b = binary.BigEndian.AppendUint16(b, word)
	return binary.BigEndian.AppendUint16(b, a.Value)
}

// DecodeProposal parses one proposal from b, consuming header + SPI +
// NumTransforms transforms. It returns the number of bytes consumed.
func DecodeProposal(b []byte) (*Proposal, int, error) {
	if len(b) < 4 {
		return nil, 0, errPayloadTooShort("proposal")
	}
	p := &Proposal{
		ProposalNum:   b[0] & 0x7f,
		ProtocolID:    b[1],
		SPISize:       b[2],
		NumTransforms: b[3],
	}
	pos := 4
	if int(p.SPISize) > 0 && pos+int(p.SPISize) <= len(b) {
		p.SPI = append([]byte{}, b[pos:pos+int(p.SPISize)]...)
		pos += int(p.SPISize)
	}
	for i := 0; i < int(p.NumTransforms) && pos+4 <= len(b); i++ {
		t, n, err := DecodeTransform(b[pos:])
		if err != nil {
			return nil, 0, err
		}
		p.Transforms = append(p.Transforms, t)
		pos += n
	}
	return p, pos, nil
}

// DecodeTransform parses one transform from b (b includes the 2-byte length
// prefix). It returns the transform and the number of bytes consumed.
func DecodeTransform(b []byte) (*Transform, int, error) {
	if len(b) < 4 {
		return nil, 0, errPayloadTooShort("transform")
	}
	length := int(binary.BigEndian.Uint16(b[0:2]))
	if length < 2 || 2+length > len(b) {
		return nil, 0, fmt.Errorf("ikev2: bad transform length %d", length)
	}
	total := 2 + length
	t := &Transform{
		TransformType: b[2],
		TransformID:   binary.BigEndian.Uint16(b[4:6]),
	}
	pos := 6
	for pos+2 <= total {
		// attribute: 16-bit header (bit15 = value present)
		hdr := binary.BigEndian.Uint16(b[pos : pos+2])
		pos += 2
		typ := hdr & 0x7fff
		if hdr&0x8000 != 0 {
			// 2-byte value
			if pos+2 > total {
				return nil, 0, errPayloadTooShort("transform attribute")
			}
			t.Attributes = append(t.Attributes, &TransformAttribute{Type: typ, Value: binary.BigEndian.Uint16(b[pos : pos+2])})
			pos += 2
		} else {
			// length-prefixed value
			if pos+2 > total {
				return nil, 0, errPayloadTooShort("transform attribute")
			}
			alen := int(binary.BigEndian.Uint16(b[pos : pos+2]))
			if pos+2+alen > total {
				return nil, 0, errPayloadTooShort("transform attribute")
			}
			pos += 2 + alen
		}
	}
	return t, total, nil
}

// CreateMultiProposalIKE builds a proposal list for the IKE SA with the given
// encryption, PRF, integrity and DH transform IDs.
func CreateMultiProposalIKE(encr, prf, integ, dh uint16) []*Proposal {
	p := &Proposal{ProposalNum: 1, ProtocolID: ProtoIKE}
	p.AddTransform(TypeEncryption, encr)
	p.AddTransform(TypePRF, prf)
	p.AddTransform(TypeIntegrity, integ)
	p.AddTransform(TypeDHGroup, dh)
	return []*Proposal{p}
}

// CreateMultiProposalESP builds a proposal list for a Child SA (ESP).
func CreateMultiProposalESP(encr, integ, dh, esn uint16) []*Proposal {
	p := &Proposal{ProposalNum: 1, ProtocolID: ProtoESP}
	p.AddTransform(TypeEncryption, encr)
	p.AddTransform(TypeIntegrity, integ)
	p.AddTransform(TypeDHGroup, dh)
	p.AddTransform(TypeESN, esn)
	return []*Proposal{p}
}
