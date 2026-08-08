package ikev2

import (
	"encoding/binary"
	"errors"
)

type RawPayload struct {
	PType PayloadType
	Data  []byte
}

func NewRawPayload(payloadType PayloadType, data []byte) *RawPayload {
	return &RawPayload{PType: payloadType, Data: data}
}

func (p *RawPayload) Type() PayloadType       { return p.PType }
func (p *RawPayload) Encode() ([]byte, error) { return p.Data, nil }

// EncryptedPayloadSK is an additive representation of an undecrypted SK body.
type EncryptedPayloadSK struct {
	NextPayload PayloadType
	Data        []byte
}

func NewEncryptedPayloadSK(next PayloadType, data []byte) *EncryptedPayloadSK {
	return &EncryptedPayloadSK{NextPayload: next, Data: data}
}

func (p *EncryptedPayloadSK) Type() PayloadType        { return SK }
func (p *EncryptedPayloadSK) Encode() ([]byte, error)  { return p.Data, nil }
func (p *EncryptedPayloadSK) payloadNext() PayloadType { return p.NextPayload }

type nextPayloadOverride interface {
	payloadNext() PayloadType
}

type EncryptedPayloadKE struct {
	DHGroup AlgorithmType
	KEData  []byte

	DHGroupNum uint16
	KeyData    []byte
}

func (p *EncryptedPayloadKE) Type() PayloadType { return KE }

func (p *EncryptedPayloadKE) Encode() ([]byte, error) {
	group := p.DHGroup
	if group == 0 {
		group = AlgorithmType(p.DHGroupNum)
	}
	data := p.KEData
	if data == nil {
		data = p.KeyData
	}
	buf := make([]byte, 4+len(data))
	binary.BigEndian.PutUint16(buf[0:2], uint16(group))
	copy(buf[4:], data)
	return buf, nil
}

func DecodePayloadKE(data []byte) (*EncryptedPayloadKE, error) {
	if len(data) < 4 {
		return nil, errors.New("KE 载荷太短")
	}
	group := AlgorithmType(binary.BigEndian.Uint16(data[0:2]))
	key := data[4:]
	return &EncryptedPayloadKE{DHGroup: group, KEData: key, DHGroupNum: uint16(group), KeyData: key}, nil
}

type EncryptedPayloadNonce struct {
	NonceData []byte
	Data      []byte
}

func (p *EncryptedPayloadNonce) Type() PayloadType { return NiNr }

func (p *EncryptedPayloadNonce) Encode() ([]byte, error) {
	if p.NonceData != nil {
		return p.NonceData, nil
	}
	return p.Data, nil
}

func DecodePayloadNonce(data []byte) (*EncryptedPayloadNonce, error) {
	return &EncryptedPayloadNonce{NonceData: data, Data: data}, nil
}

type EncryptedPayloadID struct {
	IDType      uint8
	Reserved    [3]uint8
	IDData      []byte
	IsInitiator bool

	PayloadType PayloadType
	Data        []byte
}

const (
	ID_IPV4_ADDR   uint8 = 1
	ID_FQDN        uint8 = 2
	ID_RFC822_ADDR uint8 = 3
	ID_IPV6_ADDR   uint8 = 5
	ID_DER_ASN1_DN uint8 = 9
	ID_DER_ASN1_GN uint8 = 10
	ID_KEY_ID      uint8 = 11
)

const (
	IDTypeIPv4Address = ID_IPV4_ADDR
	IDTypeFQDN        = ID_FQDN
	IDTypeRFC822Addr  = ID_RFC822_ADDR
	IDTypeIPv6Address = ID_IPV6_ADDR
)

func (p *EncryptedPayloadID) Type() PayloadType {
	if p.PayloadType != 0 {
		return p.PayloadType
	}
	if p.IsInitiator {
		return IDi
	}
	return IDr
}

func (p *EncryptedPayloadID) Encode() ([]byte, error) {
	data := p.IDData
	if data == nil {
		data = p.Data
	}
	buf := make([]byte, 4+len(data))
	buf[0] = p.IDType
	copy(buf[1:4], p.Reserved[:])
	copy(buf[4:], data)
	return buf, nil
}

func DecodePayloadID(data []byte, isInitiator bool) (*EncryptedPayloadID, error) {
	if len(data) < 4 {
		return nil, errors.New("ID 载荷太短")
	}
	payloadType := IDr
	if isInitiator {
		payloadType = IDi
	}
	payload := &EncryptedPayloadID{
		IDType: data[0], IDData: data[4:], IsInitiator: isInitiator,
		PayloadType: payloadType, Data: data[4:],
	}
	copy(payload.Reserved[:], data[1:4])
	return payload, nil
}

type EncryptedPayloadAuth struct {
	AuthMethod uint8
	AuthData   []byte
	Data       []byte
}

const (
	AuthMethodRSASig           uint8 = 1
	AuthMethodSharedKey        uint8 = 2
	AuthMethodDSSSig           uint8 = 3
	AuthMethodDigitalSignature uint8 = 14
	AuthMethodRSA                    = AuthMethodRSASig
	AuthMethodPSK                    = AuthMethodSharedKey
	AuthMethodDSS                    = AuthMethodDSSSig
	AuthMethodEAP                    = AuthMethodDigitalSignature
)

func (p *EncryptedPayloadAuth) Type() PayloadType { return AUTH }

func (p *EncryptedPayloadAuth) Encode() ([]byte, error) {
	data := p.AuthData
	if data == nil {
		data = p.Data
	}
	buf := make([]byte, 4+len(data))
	buf[0] = p.AuthMethod
	copy(buf[4:], data)
	return buf, nil
}

func DecodePayloadAuth(data []byte) (*EncryptedPayloadAuth, error) {
	if len(data) < 4 {
		return nil, errors.New("认证载荷太短")
	}
	return &EncryptedPayloadAuth{AuthMethod: data[0], AuthData: data[4:], Data: data[4:]}, nil
}

type EncryptedPayloadEAP struct {
	EAPMessage []byte
	Data       []byte
}

func (p *EncryptedPayloadEAP) Type() PayloadType { return EAP }

func (p *EncryptedPayloadEAP) Encode() ([]byte, error) {
	if p.EAPMessage != nil {
		return p.EAPMessage, nil
	}
	return p.Data, nil
}

func DecodePayloadEAP(data []byte) (*EncryptedPayloadEAP, error) {
	return &EncryptedPayloadEAP{EAPMessage: data, Data: data}, nil
}

func decodePayloadBody(payloadType PayloadType, header *PayloadHeader, body []byte) (Payload, error) {
	switch payloadType {
	case SA:
		return DecodePayloadSA(body)
	case KE:
		return DecodePayloadKE(body)
	case NiNr:
		return DecodePayloadNonce(body)
	case IDi:
		return DecodePayloadID(body, true)
	case IDr:
		return DecodePayloadID(body, false)
	case AUTH:
		return DecodePayloadAuth(body)
	case EAP:
		return DecodePayloadEAP(body)
	case N:
		return DecodePayloadNotify(body)
	case D:
		return DecodePayloadDelete(body)
	case TSI:
		return DecodePayloadTS(body, true)
	case TSR:
		return DecodePayloadTS(body, false)
	case CP:
		return DecodePayloadCP(body)
	case SK:
		return &EncryptedPayloadSK{NextPayload: header.NextPayload, Data: body}, nil
	default:
		return &RawPayload{PType: payloadType, Data: body}, nil
	}
}
