package ikev2

import (
	"encoding/binary"
	"fmt"
	"net"
)

// Traffic selector types (RFC 7296 §3.13.1).
const (
	TSIPv4Range byte = 7
	TSIPv6Range byte = 8
)

// Configuration payload types (RFC 7296 §3.15.1).
const (
	CPTypeRequest byte = 1 // CFG_REQUEST
	CPTypeReply   byte = 2 // CFG_REPLY
	CPTypeSet     byte = 3 // CFG_SET
	CPTypeAck     byte = 4 // CFG_ACK
)

// Configuration payload attribute types (RFC 7296 §3.15.1).
const (
	CPAttrIP4Address uint16 = 1  // INTERNAL_IP4_ADDRESS
	CPAttrIP4Netmask uint16 = 2  // INTERNAL_IP4_NETMASK
	CPAttrIP4DNS     uint16 = 3  // INTERNAL_IP4_DNS
	CPAttrIP6Address uint16 = 8  // INTERNAL_IP6_ADDRESS
	CPAttrIP6DNS     uint16 = 10 // INTERNAL_IP6_DNS
)

// TrafficSelector is a TS payload traffic selector.
type TrafficSelector struct {
	Type       byte
	ProtocolID byte
	StartPort  uint16
	EndPort    uint16
	StartAddr  []byte
	EndAddr    []byte
}

// NewTrafficSelectorIPV4 builds a single IPv4 address range selector.
func NewTrafficSelectorIPV4(ip net.IP, protocolID byte, startPort, endPort uint16) *TrafficSelector {
	return NewTrafficSelectorIPV4Range(ip, ip, protocolID, startPort, endPort)
}

// NewTrafficSelectorIPV4Range builds an IPv4 address range selector.
func NewTrafficSelectorIPV4Range(startIP, endIP net.IP, protocolID byte, startPort, endPort uint16) *TrafficSelector {
	start := append([]byte(nil), startIP.To4()...)
	end := append([]byte(nil), endIP.To4()...)
	return &TrafficSelector{
		Type:       TSIPv4Range,
		ProtocolID: protocolID,
		StartPort:  startPort,
		EndPort:    endPort,
		StartAddr:  start,
		EndAddr:    end,
	}
}

// NewTrafficSelectorIPV6 builds a single IPv6 address range selector.
func NewTrafficSelectorIPV6(ip net.IP, protocolID byte, startPort, endPort uint16) *TrafficSelector {
	return NewTrafficSelectorIPV6Range(ip, ip, protocolID, startPort, endPort)
}

// NewTrafficSelectorIPV6Range builds an IPv6 address range selector.
func NewTrafficSelectorIPV6Range(startIP, endIP net.IP, protocolID byte, startPort, endPort uint16) *TrafficSelector {
	start := append([]byte(nil), startIP.To16()...)
	end := append([]byte(nil), endIP.To16()...)
	return &TrafficSelector{
		Type:       TSIPv6Range,
		ProtocolID: protocolID,
		StartPort:  startPort,
		EndPort:    endPort,
		StartAddr:  start,
		EndAddr:    end,
	}
}

// Encode serialises the traffic selector.
func (t *TrafficSelector) Encode(b []byte) []byte {
	body := []byte{t.Type, t.ProtocolID}
	selectorLength := 8 + len(t.StartAddr) + len(t.EndAddr)
	body = binary.BigEndian.AppendUint16(body, uint16(selectorLength))
	body = binary.BigEndian.AppendUint16(body, t.StartPort)
	body = binary.BigEndian.AppendUint16(body, t.EndPort)
	body = append(body, t.StartAddr...)
	body = append(body, t.EndAddr...)
	return append(b, body...)
}

// CPAttribute is a Configuration payload attribute (RFC 7296 §3.15.1).
type CPAttribute struct {
	Type  uint16
	Value []byte
}

// Encode serialises the attribute.
func (a *CPAttribute) Encode(b []byte) []byte {
	b = binary.BigEndian.AppendUint16(b, a.Type)
	b = binary.BigEndian.AppendUint16(b, uint16(len(a.Value)))
	return append(b, a.Value...)
}

// decodeCPAttribute parses one configuration attribute.
func decodeCPAttribute(b []byte) (*CPAttribute, int, error) {
	if len(b) < 4 {
		return nil, 0, errPayloadTooShort("cp attribute")
	}
	typ := binary.BigEndian.Uint16(b[0:2])
	length := int(binary.BigEndian.Uint16(b[2:4]))
	if length > len(b)-4 {
		return nil, 0, errPayloadTooShort("cp attribute")
	}
	a := &CPAttribute{Type: typ, Value: append([]byte{}, b[4:4+length]...)}
	return a, 4 + length, nil
}

// CPConfig is a parsed Configuration payload (CP).
type CPConfig struct {
	Type  byte
	Attrs map[uint16][]byte
	// IPv4 / IPv6 are the assigned inner addresses, populated by HasIPv4 /
	// HasIPv6.
	IPv4 net.IP
	IPv6 net.IP
}

// ParseCPConfig parses a configuration payload into a lookup map.
func ParseCPConfig(cp *EncryptedPayloadCP) *CPConfig {
	cfg := &CPConfig{Type: cp.ConfigType, Attrs: make(map[uint16][]byte)}
	for _, a := range cp.Attrs {
		cfg.Attrs[a.Type] = a.Value
	}
	return cfg
}

// HasIPv4 reports whether the configuration includes an INTERNAL_IP4_ADDRESS
// and records it on the config.
func (c *CPConfig) HasIPv4() bool {
	v, ok := c.Attrs[CPAttrIP4Address]
	if ok && len(v) >= 4 {
		c.IPv4 = net.IP(v[:4])
	}
	return ok
}

// HasIPv6 reports whether the configuration includes an INTERNAL_IP6_ADDRESS
// and records it on the config.
func (c *CPConfig) HasIPv6() bool {
	v, ok := c.Attrs[CPAttrIP6Address]
	if ok && len(v) >= 16 {
		c.IPv6 = net.IP(v[:16])
	}
	return ok
}

var _ = fmt.Sprintf
