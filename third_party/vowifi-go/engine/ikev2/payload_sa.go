package ikev2

import (
	"encoding/binary"
	"errors"
	"fmt"
)

const (
	PROPOSAL_HEADER_LEN  = 8
	TRANSFORM_HEADER_LEN = 8
)

type EncryptedPayloadSA struct {
	Proposals []*Proposal
}

func (p *EncryptedPayloadSA) Type() PayloadType { return SA }

func (p *EncryptedPayloadSA) Encode() ([]byte, error) {
	var body []byte
	for index, proposal := range p.Proposals {
		if proposal == nil {
			return nil, fmt.Errorf("SA proposal %d 为 nil", index)
		}
		proposal.LastProposal = index == len(p.Proposals)-1
		encoded, err := proposal.Encode()
		if err != nil {
			return nil, err
		}
		body = append(body, encoded...)
	}
	return body, nil
}

type Proposal struct {
	LastProposal bool
	ProposalNum  uint8
	ProtocolID   ProtocolID
	SPI          []byte
	Transforms   []*Transform

	SPISize       uint8
	NumTransforms uint8
}

func NewProposal(number uint8, protocolID ProtocolID, spi []byte) *Proposal {
	return &Proposal{ProposalNum: number, ProtocolID: protocolID, SPI: spi, SPISize: uint8(len(spi))}
}

func (p *Proposal) Encode() ([]byte, error) {
	if p.SPISize != 0 && int(p.SPISize) != len(p.SPI) {
		return nil, fmt.Errorf("Proposal SPI 长度为 %d，声明为 %d", len(p.SPI), p.SPISize)
	}
	if p.NumTransforms != 0 && int(p.NumTransforms) != len(p.Transforms) {
		return nil, fmt.Errorf("Proposal transform 数量为 %d，声明为 %d", len(p.Transforms), p.NumTransforms)
	}
	transforms, err := p.encodeTransforms()
	if err != nil {
		return nil, err
	}
	totalLength := PROPOSAL_HEADER_LEN + len(p.SPI) + len(transforms)
	if totalLength > maxPayloadLength {
		return nil, errors.New("Proposal 长度超过 uint16")
	}
	buf := make([]byte, PROPOSAL_HEADER_LEN+len(p.SPI))
	if !p.LastProposal {
		buf[0] = 2
	}
	binary.BigEndian.PutUint16(buf[2:4], uint16(totalLength))
	buf[4] = p.ProposalNum
	buf[5] = uint8(p.ProtocolID)
	buf[6] = uint8(len(p.SPI))
	buf[7] = uint8(len(p.Transforms))
	copy(buf[PROPOSAL_HEADER_LEN:], p.SPI)
	p.SPISize = uint8(len(p.SPI))
	p.NumTransforms = uint8(len(p.Transforms))
	return append(buf, transforms...), nil
}

func (p *Proposal) encodeTransforms() ([]byte, error) {
	var body []byte
	for index, transform := range p.Transforms {
		if transform == nil {
			return nil, fmt.Errorf("Proposal transform %d 为 nil", index)
		}
		transform.LastTransform = index == len(p.Transforms)-1
		encoded, err := transform.Encode()
		if err != nil {
			return nil, err
		}
		body = append(body, encoded...)
	}
	return body, nil
}

type Transform struct {
	LastTransform bool
	Type          TransformType
	ID            AlgorithmType
	Attributes    []*TransformAttribute

	TransformType uint8
	TransformID   uint16
}

func (t *Transform) Encode() ([]byte, error) {
	t.syncOriginalFields()
	var attributes []byte
	for index, attribute := range t.Attributes {
		if attribute == nil {
			return nil, fmt.Errorf("Transform attribute %d 为 nil", index)
		}
		encoded, err := attribute.Encode()
		if err != nil {
			return nil, err
		}
		attributes = append(attributes, encoded...)
	}
	length := TRANSFORM_HEADER_LEN + len(attributes)
	if length > maxPayloadLength {
		return nil, errors.New("Transform 长度超过 uint16")
	}
	buf := make([]byte, TRANSFORM_HEADER_LEN)
	if !t.LastTransform {
		buf[0] = 3
	}
	binary.BigEndian.PutUint16(buf[2:4], uint16(length))
	buf[4] = uint8(t.Type)
	binary.BigEndian.PutUint16(buf[6:8], uint16(t.ID))
	return append(buf, attributes...), nil
}

func (t *Transform) syncOriginalFields() {
	if t.Type == 0 {
		t.Type = TransformType(t.TransformType)
	}
	if t.ID == 0 {
		t.ID = AlgorithmType(t.TransformID)
	}
	t.TransformType = uint8(t.Type)
	t.TransformID = uint16(t.ID)
}

type TransformAttribute struct {
	Type  uint16
	Value []byte
	Val   uint16
}

func (a *TransformAttribute) Encode() ([]byte, error) {
	if len(a.Value) == 0 {
		buf := make([]byte, 4)
		binary.BigEndian.PutUint16(buf[0:2], a.Type|0x8000)
		binary.BigEndian.PutUint16(buf[2:4], a.Val)
		return buf, nil
	}
	if len(a.Value) > maxPayloadLength {
		return nil, errors.New("Transform Attribute 长度超过 uint16")
	}
	buf := make([]byte, 4+len(a.Value))
	binary.BigEndian.PutUint16(buf[0:2], a.Type&0x7fff)
	binary.BigEndian.PutUint16(buf[2:4], uint16(len(a.Value)))
	copy(buf[4:], a.Value)
	return buf, nil
}

func (p *Proposal) AddTransform(transformType TransformType, transformID AlgorithmType, keyLength ...int) {
	attributes := []*TransformAttribute(nil)
	if len(keyLength) > 0 && keyLength[0] > 0 {
		attributes = append(attributes, &TransformAttribute{Type: AttributeKeyLength, Val: uint16(keyLength[0])})
	}
	p.Transforms = append(p.Transforms, newTransform(transformType, transformID, attributes))
	p.NumTransforms = uint8(len(p.Transforms))
}

func (p *Proposal) AddTransformWithKeyLen(transformType TransformType, transformID AlgorithmType, keyLength int) {
	p.AddTransform(transformType, transformID, keyLength)
}

func newTransform(transformType TransformType, transformID AlgorithmType, attributes []*TransformAttribute) *Transform {
	return &Transform{
		Type: transformType, ID: transformID, Attributes: attributes,
		TransformType: uint8(transformType), TransformID: uint16(transformID),
	}
}

func DecodePayloadSA(data []byte) (*EncryptedPayloadSA, error) {
	sa := &EncryptedPayloadSA{}
	for len(data) > 0 {
		proposal, consumed, err := decodeProposal(data)
		if err != nil {
			return nil, err
		}
		sa.Proposals = append(sa.Proposals, proposal)
		data = data[consumed:]
		if proposal.LastProposal {
			if len(data) != 0 {
				return nil, errors.New("最后一个 Proposal 后仍有数据")
			}
			break
		}
	}
	return sa, nil
}

func DecodeProposal(data []byte) (*Proposal, error) {
	proposal, _, err := decodeProposal(data)
	return proposal, err
}

func DecodeProposalWithLength(data []byte) (*Proposal, int, error) {
	return decodeProposal(data)
}

func decodeProposal(data []byte) (*Proposal, int, error) {
	if len(data) < PROPOSAL_HEADER_LEN {
		return nil, 0, errors.New("Proposal too short")
	}
	length := int(binary.BigEndian.Uint16(data[2:4]))
	if length < PROPOSAL_HEADER_LEN || length > len(data) {
		return nil, 0, errors.New("Proposal too short for body")
	}
	spiSize := int(data[6])
	if PROPOSAL_HEADER_LEN+spiSize > length {
		return nil, 0, errors.New("Proposal too short for SPI")
	}
	proposal := &Proposal{
		LastProposal: data[0] == 0, ProposalNum: data[4], ProtocolID: ProtocolID(data[5]),
		SPI:     append([]byte(nil), data[PROPOSAL_HEADER_LEN:PROPOSAL_HEADER_LEN+spiSize]...),
		SPISize: data[6], NumTransforms: data[7],
	}
	offset := PROPOSAL_HEADER_LEN + spiSize
	for range int(proposal.NumTransforms) {
		transform, consumed, err := decodeTransform(data[offset:length])
		if err != nil {
			return nil, 0, err
		}
		proposal.Transforms = append(proposal.Transforms, transform)
		offset += consumed
	}
	if offset != length {
		return nil, 0, fmt.Errorf("Proposal 包含 %d 个尾随字节", length-offset)
	}
	return proposal, length, nil
}

func DecodeTransform(data []byte) (*Transform, error) {
	transform, _, err := decodeTransform(data)
	return transform, err
}

func DecodeTransformWithLength(data []byte) (*Transform, int, error) {
	return decodeTransform(data)
}

func decodeTransform(data []byte) (*Transform, int, error) {
	if len(data) < TRANSFORM_HEADER_LEN {
		return nil, 0, errors.New("Transform too short")
	}
	length := int(binary.BigEndian.Uint16(data[2:4]))
	if length < TRANSFORM_HEADER_LEN || length > len(data) {
		return nil, 0, errors.New("Transform too short for body")
	}
	transformType := TransformType(data[4])
	transformID := AlgorithmType(binary.BigEndian.Uint16(data[6:8]))
	transform := newTransform(transformType, transformID, nil)
	transform.LastTransform = data[0] == 0
	for offset := TRANSFORM_HEADER_LEN; offset < length; {
		attribute, consumed, err := decodeTransformAttribute(data[offset:length])
		if err != nil {
			return nil, 0, err
		}
		transform.Attributes = append(transform.Attributes, attribute)
		offset += consumed
	}
	return transform, length, nil
}

func decodeTransformAttribute(data []byte) (*TransformAttribute, int, error) {
	if len(data) < 4 {
		return nil, 0, errors.New("Transform too short for Attribute header")
	}
	rawType := binary.BigEndian.Uint16(data[0:2])
	attribute := &TransformAttribute{Type: rawType & 0x7fff}
	if rawType&0x8000 != 0 {
		attribute.Val = binary.BigEndian.Uint16(data[2:4])
		return attribute, 4, nil
	}
	length := int(binary.BigEndian.Uint16(data[2:4]))
	if 4+length > len(data) {
		return nil, 0, errors.New("Transform Attribute value truncated")
	}
	attribute.Value = append([]byte(nil), data[4:4+length]...)
	return attribute, 4 + length, nil
}
