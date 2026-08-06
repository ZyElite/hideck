// Package ikev2 implements the IKEv2 message and payload encoding used by the
// vowifi SWu (UE <-> ePDG) client.
//
// Reconstructed from the decompiled engine/ikev2 and RFC 7296. This file
// covers the common payload header and the packet framing:
//
//	IKE header (28 bytes, RFC 7296 §3.1):
//	  [ 0] Initiator SPI   (8)
//	  [ 8] Responder SPI   (8)
//	  [16] Next Payload    (1)
//	  [17] Version         (1)  0x20 = IKEv2
//	  [18] Exchange Type   (1)
//	  [19] Flags           (1)
//	  [20] Message ID      (4)
//	  [24] Length          (4)
//
//	generic payload header (RFC 7296 §3.2, 4 bytes):
//	  Next Payload (1) | Critical/Reserved (1) | Payload Length (2)
package ikev2

import (
	"encoding/binary"
	"fmt"
)

// Payload type codes (RFC 7296 §3.3.1).
const (
	PayloadNoNext    byte = 0
	PayloadSA        byte = 33
	PayloadKE        byte = 34
	PayloadIDi       byte = 35
	PayloadIDr       byte = 36
	PayloadCert      byte = 37
	PayloadCertReq   byte = 38
	PayloadAuth      byte = 39
	PayloadNi        byte = 40
	PayloadNotify    byte = 41
	PayloadDelete    byte = 42
	PayloadVendorID  byte = 43
	PayloadTS        byte = 44
	PayloadTSi       byte = 44
	PayloadTSr       byte = 45
	PayloadCP        byte = 46
	PayloadEncrypted byte = 46
	PayloadEAP       byte = 48
)

// Exchange types (RFC 7296 §3.6.1).
const (
	ExchangeIKEInit       byte = 34 // IKE_SA_INIT
	ExchangeIKEAuth       byte = 35 // IKE_AUTH
	ExchangeCreateChildSA byte = 36 // CREATE_CHILD_SA
	ExchangeInformational byte = 37 // INFORMATIONAL
)

// Payload is an IKEv2 payload. Encode appends the fully-encoded payload
// (generic 4-byte header plus body) to b.
type Payload interface {
	// Type returns the payload type code.
	Type() byte
	// Encode appends the encoded payload (with generic header) to b.
	Encode(b []byte) []byte
}

// payload is embedded in each concrete payload; NextPayload is set by the
// packet encoder when chaining payloads.
type payload struct {
	NextPayload byte
	Critical    bool
}

// setNextPayload lets the packet encoder chain payloads.
func (p *payload) setNextPayload(b byte) { p.NextPayload = b }

// encode writes the generic header followed by body.
func (p *payload) encode(b []byte, body []byte) []byte {
	b = encodeHeader(b, p.NextPayload, p.Critical, len(body)+4)
	return append(b, body...)
}

// PayloadHeader is the 4-byte generic payload header.
type PayloadHeader struct {
	NextPayload byte
	Critical    bool
	Length      uint16
}

// encodeHeader writes the generic 4-byte payload header.
func encodeHeader(b []byte, next byte, critical bool, length int) []byte {
	h := make([]byte, 4)
	h[0] = next
	if critical {
		h[1] = 0x80
	}
	binary.BigEndian.PutUint16(h[2:4], uint16(length))
	return append(b, h...)
}

// decodeHeader parses the generic payload header.
func decodeHeader(b []byte) (PayloadHeader, error) {
	if len(b) < 4 {
		return PayloadHeader{}, errPayloadTooShort("header")
	}
	return PayloadHeader{
		NextPayload: b[0],
		Critical:    b[1]&0x80 != 0,
		Length:      binary.BigEndian.Uint16(b[2:4]),
	}, nil
}

// errPayloadTooShort is the error returned when a payload is truncated.
type errPayloadTooShort string

func (e errPayloadTooShort) Error() string { return fmt.Sprintf("ikev2: %s too short", string(e)) }

// IKEPacket is a full IKEv2 packet: header plus a chain of payloads.
type IKEPacket struct {
	InitiatorSPI [8]byte
	ResponderSPI [8]byte
	NextPayload  byte
	Version      byte
	ExchangeType byte
	Flags        byte
	MessageID    uint32
	Length       uint32
	Payloads     []Payload
}

// Encode serialises the packet to bytes, chaining the payloads.
func (p *IKEPacket) Encode() []byte {
	body := encodePayloadChain(nil, p.Payloads)
	next := byte(PayloadNoNext)
	if len(p.Payloads) > 0 {
		next = p.Payloads[0].Type()
	}

	b := make([]byte, 28)
	copy(b[0:8], p.InitiatorSPI[:])
	copy(b[8:16], p.ResponderSPI[:])
	b[16] = next
	b[17] = p.Version
	if b[17] == 0 {
		b[17] = 0x20 // IKEv2
	}
	b[18] = p.ExchangeType
	b[19] = p.Flags
	binary.BigEndian.PutUint32(b[20:24], p.MessageID)
	binary.BigEndian.PutUint32(b[24:28], uint32(28+len(body)))
	return append(b, body...)
}

// nextSetter is implemented by the embedded payload base.
type nextSetter interface {
	setNextPayload(b byte)
}

// EncodePayloadChain encodes a payload chain, setting NextPayload on each
// payload to the following payload's type. It is the exported form of
// encodePayloadChain, used by the SWu session to build encrypted IKE messages.
func EncodePayloadChain(payloads []Payload) []byte {
	return encodePayloadChain(nil, payloads)
}

// DecodePayloadChain parses a payload chain from an encrypted IKE message body.
func DecodePayloadChain(b []byte) ([]Payload, error) {
	var out []Payload
	for len(b) > 0 {
		pl, n, err := decodePayload(b, b[0])
		if err != nil {
			return nil, err
		}
		out = append(out, pl)
		b = b[n:]
	}
	return out, nil
}

// encodePayloadChain encodes payloads, setting NextPayload on each to the
// following payload's type.
func encodePayloadChain(b []byte, payloads []Payload) []byte {
	for i, pl := range payloads {
		if setter, ok := pl.(nextSetter); ok {
			if i+1 < len(payloads) {
				setter.setNextPayload(payloads[i+1].Type())
			} else {
				setter.setNextPayload(PayloadNoNext)
			}
		}
		b = pl.Encode(b)
	}
	return b
}

// DecodePacket parses an IKEv2 packet from b.
func DecodePacket(b []byte) (*IKEPacket, error) {
	if len(b) < 28 {
		return nil, errPayloadTooShort("packet")
	}
	p := &IKEPacket{}
	copy(p.InitiatorSPI[:], b[0:8])
	copy(p.ResponderSPI[:], b[8:16])
	p.NextPayload = b[16]
	p.Version = b[17]
	p.ExchangeType = b[18]
	p.Flags = b[19]
	p.MessageID = binary.BigEndian.Uint32(b[20:24])
	p.Length = binary.BigEndian.Uint32(b[24:28])

	end := len(b)
	if p.Length > 0 && int(p.Length) <= len(b) {
		end = int(p.Length)
	} else if p.Length > 0 {
		return nil, fmt.Errorf("ikev2: packet length %d exceeds buffer %d", p.Length, len(b))
	}

	pos := 28
	next := p.NextPayload
	for pos < end && next != PayloadNoNext {
		pl, n, err := decodePayload(b[pos:end], next)
		if err != nil {
			return nil, err
		}
		p.Payloads = append(p.Payloads, pl)
		hdr, err := decodeHeader(b[pos:end])
		if err != nil {
			return nil, err
		}
		next = hdr.NextPayload
		pos += n
	}
	return p, nil
}
