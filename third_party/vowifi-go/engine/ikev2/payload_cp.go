package ikev2

import (
	"encoding/binary"
	"errors"
	"fmt"
	"net"
)

const (
	CFG_REQUEST   uint8 = 1
	CFG_REPLY     uint8 = 2
	CFG_SET       uint8 = 3
	CFG_ACK       uint8 = 4
	CPTypeRequest       = CFG_REQUEST
	CPTypeReply         = CFG_REPLY
	CPTypeSet           = CFG_SET
	CPTypeAck           = CFG_ACK
)

const (
	INTERNAL_IP4_ADDRESS       uint16 = 1
	INTERNAL_IP4_NETMASK       uint16 = 2
	INTERNAL_IP4_DNS           uint16 = 3
	INTERNAL_IP4_NBNS          uint16 = 4
	INTERNAL_IP4_DHCP          uint16 = 6
	APPLICATION_VERSION        uint16 = 7
	INTERNAL_IP6_ADDRESS       uint16 = 8
	INTERNAL_IP6_DNS           uint16 = 10
	INTERNAL_IP6_DHCP          uint16 = 12
	INTERNAL_IP4_SUBNET        uint16 = 13
	SUPPORTED_ATTRIBUTES       uint16 = 14
	P_CSCF_IP4_ADDRESS         uint16 = 20
	P_CSCF_IP6_ADDRESS         uint16 = 21
	ASSIGNED_PCSCF_IP6_ADDRESS uint16 = 16390
)

const (
	CPAttrIP4Address = INTERNAL_IP4_ADDRESS
	CPAttrIP4Netmask = INTERNAL_IP4_NETMASK
	CPAttrIP4DNS     = INTERNAL_IP4_DNS
	CPAttrIP6Address = INTERNAL_IP6_ADDRESS
	CPAttrIP6DNS     = INTERNAL_IP6_DNS
	CPAttrPCSCFIP4   = P_CSCF_IP4_ADDRESS
	CPAttrPCSCFIP6   = P_CSCF_IP6_ADDRESS
)

type EncryptedPayloadCP struct {
	CFGType    uint8
	Attributes []*CPAttribute

	ConfigType uint8
	Attrs      []*CPAttribute
}

func (p *EncryptedPayloadCP) Type() PayloadType { return CP }

func (p *EncryptedPayloadCP) Encode() ([]byte, error) {
	configType := p.CFGType
	if configType == 0 {
		configType = p.ConfigType
	}
	attributes := p.Attributes
	if attributes == nil {
		attributes = p.Attrs
	}
	buf := make([]byte, 4)
	buf[0] = configType
	for index, attribute := range attributes {
		if attribute == nil {
			return nil, fmt.Errorf("CP attribute %d 为 nil", index)
		}
		encoded, err := attribute.Encode()
		if err != nil {
			return nil, err
		}
		buf = append(buf, encoded...)
	}
	p.CFGType = configType
	p.ConfigType = configType
	p.Attributes = attributes
	p.Attrs = attributes
	return buf, nil
}

type CPAttribute struct {
	Type  uint16
	Value []byte
}

func (a *CPAttribute) Encode() ([]byte, error) {
	if len(a.Value) > maxPayloadLength {
		return nil, errors.New("CP Attribute 长度超过 uint16")
	}
	buf := make([]byte, 4+len(a.Value))
	binary.BigEndian.PutUint16(buf[0:2], a.Type)
	binary.BigEndian.PutUint16(buf[2:4], uint16(len(a.Value)))
	copy(buf[4:], a.Value)
	return buf, nil
}

func DecodePayloadCP(data []byte) (*EncryptedPayloadCP, error) {
	if len(data) < 4 {
		return nil, errPayloadTooShort("CP")
	}
	payload := &EncryptedPayloadCP{CFGType: data[0], ConfigType: data[0]}
	for offset := 4; offset < len(data); {
		attribute, consumed, err := decodeCPAttribute(data[offset:])
		if err != nil {
			return nil, err
		}
		payload.Attributes = append(payload.Attributes, attribute)
		offset += consumed
	}
	payload.Attrs = payload.Attributes
	return payload, nil
}

func decodeCPAttribute(data []byte) (*CPAttribute, int, error) {
	if len(data) < 4 {
		return nil, 0, errPayloadTooShort("CP Attribute")
	}
	rawType := binary.BigEndian.Uint16(data[0:2])
	attributeType := rawType & 0x7fff
	if rawType&0x8000 != 0 {
		return &CPAttribute{Type: attributeType, Value: append([]byte(nil), data[2:4]...)}, 4, nil
	}
	length := int(binary.BigEndian.Uint16(data[2:4]))
	if 4+length > len(data) {
		return nil, 0, errPayloadTooShort("CP Attribute Value")
	}
	return &CPAttribute{
		Type: attributeType, Value: append([]byte(nil), data[4:4+length]...),
	}, 4 + length, nil
}

type CPConfig struct {
	IPv4Addresses []net.IP
	IPv4DNS       []net.IP
	IPv4PCSCF     []net.IP
	IPv6Addresses []net.IP
	IPv6Prefix    uint8
	IPv6DNS       []net.IP
	IPv6PCSCF     []net.IP

	Type  uint8
	Attrs map[uint16][]byte
	IPv4  net.IP
	IPv6  net.IP
}

func ParseCPConfig(payload *EncryptedPayloadCP) *CPConfig {
	config := &CPConfig{Attrs: make(map[uint16][]byte)}
	if payload == nil {
		return config
	}
	config.Type = payload.CFGType
	if config.Type == 0 {
		config.Type = payload.ConfigType
	}
	attributes := payload.Attributes
	if attributes == nil {
		attributes = payload.Attrs
	}
	for _, attribute := range attributes {
		if attribute == nil {
			continue
		}
		config.Attrs[attribute.Type] = attribute.Value
		config.addAttribute(attribute)
	}
	if len(config.IPv4Addresses) > 0 {
		config.IPv4 = config.IPv4Addresses[0]
	}
	if len(config.IPv6Addresses) > 0 {
		config.IPv6 = config.IPv6Addresses[0]
	}
	return config
}

func (c *CPConfig) addAttribute(attribute *CPAttribute) {
	value := attribute.Value
	switch attribute.Type {
	case INTERNAL_IP4_ADDRESS:
		c.IPv4Addresses = appendIP(c.IPv4Addresses, value, net.IPv4len)
	case INTERNAL_IP4_DNS:
		c.IPv4DNS = appendIP(c.IPv4DNS, value, net.IPv4len)
	case P_CSCF_IP4_ADDRESS:
		c.IPv4PCSCF = appendIP(c.IPv4PCSCF, value, net.IPv4len)
	case INTERNAL_IP6_ADDRESS:
		c.IPv6Addresses = appendIP(c.IPv6Addresses, value, net.IPv6len)
		if len(value) >= net.IPv6len+1 {
			c.IPv6Prefix = value[net.IPv6len]
		}
	case INTERNAL_IP6_DNS:
		c.IPv6DNS = appendIP(c.IPv6DNS, value, net.IPv6len)
	case P_CSCF_IP6_ADDRESS, ASSIGNED_PCSCF_IP6_ADDRESS:
		c.IPv6PCSCF = appendIP(c.IPv6PCSCF, value, net.IPv6len)
	}
}

func appendIP(addresses []net.IP, value []byte, length int) []net.IP {
	if len(value) < length {
		return addresses
	}
	return append(addresses, net.IP(value[:length]))
}

func (c *CPConfig) HasIPv4() bool { return c != nil && len(c.IPv4Addresses) > 0 }
func (c *CPConfig) HasIPv6() bool { return c != nil && len(c.IPv6Addresses) > 0 }
