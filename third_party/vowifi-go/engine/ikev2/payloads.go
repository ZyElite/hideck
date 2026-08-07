package ikev2

import (
	"encoding/binary"
	"fmt"
)

// RawPayload carries arbitrary bytes with an explicit type.
type RawPayload struct {
	payload
	payloadType byte
	Data        []byte
}

// NewRawPayload builds a raw payload with the given type and body bytes.
func NewRawPayload(typ byte, data []byte) *RawPayload {
	return &RawPayload{payloadType: typ, Data: data}
}

// Type returns the payload type this raw payload was decoded as.
func (p *RawPayload) Type() byte { return p.payloadType }

// Encode writes the raw bytes.
func (p *RawPayload) Encode(b []byte) []byte {
	return p.encode(b, p.Data)
}

// EncryptedPayloadSK carries the protected payload bytes. Its NextPayload is
// the type of the first encrypted inner payload (RFC 7296 section 3.14).
type EncryptedPayloadSK struct {
	NextPayload byte
	Data        []byte
}

// NewEncryptedPayloadSK builds an SK payload without exposing payload header
// internals to the SWu session package.
func NewEncryptedPayloadSK(next byte, data []byte) *EncryptedPayloadSK {
	return &EncryptedPayloadSK{NextPayload: next, Data: data}
}

func (p *EncryptedPayloadSK) Type() byte { return PayloadEncrypted }

func (p *EncryptedPayloadSK) Encode(b []byte) []byte {
	b = encodeHeader(b, p.NextPayload, false, len(p.Data)+4)
	return append(b, p.Data...)
}

// EncryptedPayloadSA is a Security Association payload containing proposals.
type EncryptedPayloadSA struct {
	payload
	Proposals []*Proposal
}

func (p *EncryptedPayloadSA) Type() byte { return PayloadSA }

func (p *EncryptedPayloadSA) Encode(b []byte) []byte {
	var body []byte
	for index, proposal := range p.Proposals {
		body = proposal.encode(body, index == len(p.Proposals)-1)
	}
	return p.encode(b, body)
}

// DecodePayloadSA parses an SA payload body (after the generic header).
func DecodePayloadSA(body []byte) (*EncryptedPayloadSA, error) {
	sa := &EncryptedPayloadSA{}
	pos := 0
	for pos < len(body) {
		pr, n, err := DecodeProposal(body[pos:])
		if err != nil {
			return nil, err
		}
		sa.Proposals = append(sa.Proposals, pr)
		if n == 0 {
			break
		}
		pos += n
	}
	return sa, nil
}

// EncryptedPayloadKE is a Key Exchange payload.
type EncryptedPayloadKE struct {
	payload
	DHGroupNum uint16
	KeyData    []byte
}

func (p *EncryptedPayloadKE) Type() byte { return PayloadKE }

func (p *EncryptedPayloadKE) Encode(b []byte) []byte {
	body := make([]byte, 4)
	binary.BigEndian.PutUint16(body[0:2], p.DHGroupNum)
	binary.BigEndian.PutUint16(body[2:4], 0) // reserved
	body = append(body, p.KeyData...)
	return p.encode(b, body)
}

// EncryptedPayloadNonce is a Nonce payload.
type EncryptedPayloadNonce struct {
	payload
	Data []byte
}

func (p *EncryptedPayloadNonce) Type() byte { return PayloadNi }

func (p *EncryptedPayloadNonce) Encode(b []byte) []byte {
	return p.encode(b, p.Data)
}

// EncryptedPayloadID is an Identification payload.
type EncryptedPayloadID struct {
	payload
	PayloadType byte
	IDType      byte
	Data        []byte
}

func (p *EncryptedPayloadID) Type() byte {
	if p.PayloadType == PayloadIDr {
		return PayloadIDr
	}
	return PayloadIDi
}

func (p *EncryptedPayloadID) Encode(b []byte) []byte {
	body := []byte{p.IDType, 0, 0, 0}
	body = append(body, p.Data...)
	return p.encode(b, body)
}

// Authentication method types (RFC 7296 §3.3.3).
const (
	AuthMethodRSA byte = 1  // RSA digital signature
	AuthMethodPSK byte = 2  // pre-shared key
	AuthMethodDSS byte = 3  // DSS digital signature
	AuthMethodEAP byte = 14 // EAP
)

// EncryptedPayloadAuth is an Authentication payload.
type EncryptedPayloadAuth struct {
	payload
	AuthMethod byte
	Data       []byte
}

func (p *EncryptedPayloadAuth) Type() byte { return PayloadAuth }

func (p *EncryptedPayloadAuth) Encode(b []byte) []byte {
	body := []byte{p.AuthMethod, 0, 0, 0}
	body = append(body, p.Data...)
	return p.encode(b, body)
}

// EncryptedPayloadEAP is an EAP payload.
type EncryptedPayloadEAP struct {
	payload
	Data []byte
}

func (p *EncryptedPayloadEAP) Type() byte { return PayloadEAP }

func (p *EncryptedPayloadEAP) Encode(b []byte) []byte {
	return p.encode(b, p.Data)
}

// EncryptedPayloadNotify is a Notify payload.
type EncryptedPayloadNotify struct {
	payload
	ProtocolID byte
	SPISize    byte
	NotifyType uint16
	SPI        []byte
	NotifyData []byte
}

func (p *EncryptedPayloadNotify) Type() byte { return PayloadNotify }

func (p *EncryptedPayloadNotify) Encode(b []byte) []byte {
	body := []byte{p.ProtocolID, p.SPISize}
	body = binary.BigEndian.AppendUint16(body, p.NotifyType)
	body = append(body, p.SPI...)
	body = append(body, p.NotifyData...)
	return p.encode(b, body)
}

// EncryptedPayloadDelete is a Delete payload.
type EncryptedPayloadDelete struct {
	payload
	ProtocolID byte
	SPISize    byte
	NumSPIs    uint16
	SPIs       []byte
}

func (p *EncryptedPayloadDelete) Type() byte { return PayloadDelete }

func (p *EncryptedPayloadDelete) Encode(b []byte) []byte {
	body := []byte{p.ProtocolID, p.SPISize}
	body = binary.BigEndian.AppendUint16(body, p.NumSPIs)
	body = append(body, p.SPIs...)
	return p.encode(b, body)
}

// DecodePayloadDelete parses a Delete payload body.
func DecodePayloadDelete(body []byte) (*EncryptedPayloadDelete, error) {
	if len(body) < 4 {
		return nil, errPayloadTooShort("delete")
	}
	d := &EncryptedPayloadDelete{
		ProtocolID: body[0],
		SPISize:    body[1],
		NumSPIs:    binary.BigEndian.Uint16(body[2:4]),
	}
	d.SPIs = append([]byte{}, body[4:]...)
	return d, nil
}

// EncryptedPayloadTS is a Traffic Selector payload.
type EncryptedPayloadTS struct {
	payload
	TSNumber  byte
	Selectors []*TrafficSelector
}

func (p *EncryptedPayloadTS) Type() byte { return PayloadTS }

func (p *EncryptedPayloadTS) Encode(b []byte) []byte {
	body := []byte{p.TSNumber, 0, 0, 0}
	for _, ts := range p.Selectors {
		body = ts.Encode(body)
	}
	return p.encode(b, body)
}

// DecodePayloadTS parses a Traffic Selector payload body.
func DecodePayloadTS(body []byte) (*EncryptedPayloadTS, error) {
	if len(body) < 4 {
		return nil, errPayloadTooShort("ts")
	}
	ts := &EncryptedPayloadTS{TSNumber: body[0]}
	pos := 4
	for pos+8 <= len(body) {
		n, err := ts.decodeSelector(body[pos:])
		if err != nil {
			return nil, err
		}
		pos += n
	}
	return ts, nil
}

func (p *EncryptedPayloadTS) decodeSelector(b []byte) (int, error) {
	if len(b) < 8 {
		return 0, errPayloadTooShort("ts selector")
	}
	tsType := b[0]
	addrLen := 4
	if tsType == 8 { // IPv6
		addrLen = 16
	}
	total := 8 + addrLen*2
	if len(b) < total {
		return 0, errPayloadTooShort("ts selector")
	}
	sel := &TrafficSelector{
		Type:       b[0],
		ProtocolID: b[1],
		StartPort:  binary.BigEndian.Uint16(b[2:4]),
		EndPort:    binary.BigEndian.Uint16(b[4:6]),
	}
	sel.StartAddr = append([]byte{}, b[8:8+addrLen]...)
	sel.EndAddr = append([]byte{}, b[8+addrLen:8+addrLen*2]...)
	p.Selectors = append(p.Selectors, sel)
	return total, nil
}

// EncryptedPayloadCP is a Configuration payload.
type EncryptedPayloadCP struct {
	payload
	ConfigType byte
	Attrs      []*CPAttribute
}

func (p *EncryptedPayloadCP) Type() byte { return PayloadCP }

func (p *EncryptedPayloadCP) Encode(b []byte) []byte {
	body := []byte{p.ConfigType, 0, 0, 0}
	for _, a := range p.Attrs {
		body = a.Encode(body)
	}
	return p.encode(b, body)
}

// DecodePayloadCP parses a Configuration payload body.
func DecodePayloadCP(body []byte) (*EncryptedPayloadCP, error) {
	if len(body) < 4 {
		return nil, errPayloadTooShort("cp")
	}
	cp := &EncryptedPayloadCP{ConfigType: body[0]}
	pos := 4
	for pos+4 <= len(body) {
		a, n, err := decodeCPAttribute(body[pos:])
		if err != nil {
			return nil, err
		}
		cp.Attrs = append(cp.Attrs, a)
		pos += n
	}
	return cp, nil
}

// decodePayload dispatches on the payload type and parses it.
func decodePayload(b []byte, typ byte) (Payload, int, error) {
	if len(b) < 4 {
		return nil, 0, errPayloadTooShort("payload")
	}
	hdr, err := decodeHeader(b)
	if err != nil {
		return nil, 0, err
	}
	length := int(hdr.Length)
	if length < 4 || length > len(b) {
		return nil, 0, fmt.Errorf("ikev2: bad payload length %d", length)
	}
	body := b[4:length]

	var pl Payload
	switch typ {
	case PayloadEncrypted:
		pl = &EncryptedPayloadSK{NextPayload: hdr.NextPayload, Data: append([]byte{}, body...)}
	case PayloadSA:
		pl, err = DecodePayloadSA(body)
	case PayloadTS:
		pl, err = DecodePayloadTS(body)
	case PayloadCP:
		pl, err = DecodePayloadCP(body)
	case PayloadDelete:
		pl, err = DecodePayloadDelete(body)
	case PayloadIDi, PayloadIDr:
		if len(body) < 4 {
			return nil, 0, errPayloadTooShort("identification")
		}
		pl = &EncryptedPayloadID{PayloadType: typ, IDType: body[0], Data: append([]byte{}, body[4:]...)}
	case PayloadAuth:
		if len(body) < 4 {
			return nil, 0, errPayloadTooShort("authentication")
		}
		pl = &EncryptedPayloadAuth{AuthMethod: body[0], Data: append([]byte{}, body[4:]...)}
	case PayloadEAP:
		pl = &EncryptedPayloadEAP{Data: append([]byte{}, body...)}
	case PayloadKE, PayloadNi, PayloadNotify, PayloadVendorID:
		rp := &RawPayload{payloadType: typ, Data: append([]byte{}, body...)}
		rp.NextPayload = hdr.NextPayload
		pl = rp
	default:
		rp := &RawPayload{payloadType: typ, Data: append([]byte{}, body...)}
		rp.NextPayload = hdr.NextPayload
		pl = rp
	}
	if err != nil {
		return nil, 0, err
	}
	return pl, length, nil
}
