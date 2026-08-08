package ikev2

import (
	"encoding/binary"
	"errors"
	"fmt"
	"net"
)

const (
	TS_IPV4_ADDR_RANGE uint8 = 7
	TS_IPV6_ADDR_RANGE uint8 = 8
	TSIPv4Range              = TS_IPV4_ADDR_RANGE
	TSIPv6Range              = TS_IPV6_ADDR_RANGE
)

type EncryptedPayloadTS struct {
	IsInitiator      bool
	TrafficSelectors []*TrafficSelector

	PayloadType PayloadType
	TSNumber    uint8
	Selectors   []*TrafficSelector
}

func (p *EncryptedPayloadTS) Type() PayloadType {
	if p.PayloadType != 0 {
		return p.PayloadType
	}
	if p.IsInitiator {
		return TSI
	}
	return TSR
}

func (p *EncryptedPayloadTS) Encode() ([]byte, error) {
	selectors := p.TrafficSelectors
	if selectors == nil {
		selectors = p.Selectors
	}
	count := len(selectors)
	if p.TSNumber != 0 && int(p.TSNumber) != count {
		return nil, fmt.Errorf("TS 数量为 %d，声明为 %d", count, p.TSNumber)
	}
	if count > 255 {
		return nil, errors.New("TS 数量超过 uint8")
	}
	buf := make([]byte, 4)
	buf[0] = uint8(count)
	for index, selector := range selectors {
		if selector == nil {
			return nil, fmt.Errorf("Traffic Selector %d 为 nil", index)
		}
		buf = append(buf, selector.Encode()...)
	}
	p.TrafficSelectors = selectors
	p.Selectors = selectors
	p.TSNumber = uint8(count)
	return buf, nil
}

type TrafficSelector struct {
	TSType     uint8
	IPProtocol uint8
	StartPort  uint16
	EndPort    uint16
	StartAddr  []byte
	EndAddr    []byte

	Type       uint8
	ProtocolID uint8
}

// NewTrafficSelectorIPV4 supports both the original start/end form and the
// interim single-address/protocol form.
func NewTrafficSelectorIPV4(startIP net.IP, endOrProtocol any, startPort, endPort uint16) *TrafficSelector {
	if endIP, ok := endOrProtocol.(net.IP); ok {
		return NewTrafficSelectorIPV4Range(startIP, endIP, 0, startPort, endPort)
	}
	return NewTrafficSelectorIPV4Range(startIP, startIP, protocolArgument(endOrProtocol), startPort, endPort)
}

func NewTrafficSelectorIPV4Range(startIP, endIP net.IP, protocol uint8, startPort, endPort uint16) *TrafficSelector {
	return newTrafficSelector(TS_IPV4_ADDR_RANGE, protocol, startIP.To4(), endIP.To4(), startPort, endPort)
}

func NewTrafficSelectorIPV6(startIP net.IP, endOrProtocol any, startPort, endPort uint16) *TrafficSelector {
	if endIP, ok := endOrProtocol.(net.IP); ok {
		return NewTrafficSelectorIPV6Range(startIP, endIP, 0, startPort, endPort)
	}
	return NewTrafficSelectorIPV6Range(startIP, startIP, protocolArgument(endOrProtocol), startPort, endPort)
}

func NewTrafficSelectorIPV6Range(startIP, endIP net.IP, protocol uint8, startPort, endPort uint16) *TrafficSelector {
	return newTrafficSelector(TS_IPV6_ADDR_RANGE, protocol, startIP.To16(), endIP.To16(), startPort, endPort)
}

func newTrafficSelector(selectorType, protocol uint8, start, end []byte, startPort, endPort uint16) *TrafficSelector {
	return &TrafficSelector{
		TSType: selectorType, IPProtocol: protocol, StartPort: startPort, EndPort: endPort,
		StartAddr: append([]byte(nil), start...), EndAddr: append([]byte(nil), end...),
		Type: selectorType, ProtocolID: protocol,
	}
}

func protocolArgument(argument any) uint8 {
	switch value := argument.(type) {
	case uint8:
		return value
	case int:
		if value >= 0 && value <= 255 {
			return uint8(value)
		}
	}
	panic(fmt.Sprintf("IP protocol has unsupported type/value %T", argument))
}

func (t *TrafficSelector) Encode() []byte {
	t.syncOriginalFields()
	addressLength := net.IPv4len
	switch t.TSType {
	case TS_IPV4_ADDR_RANGE:
	case TS_IPV6_ADDR_RANGE:
		addressLength = net.IPv6len
	default:
		panic(fmt.Sprintf("不支持的 TS 类型 %d", t.TSType))
	}
	if len(t.StartAddr) != addressLength || len(t.EndAddr) != addressLength {
		panic(fmt.Sprintf("TS 类型 %d 的地址长度必须为 %d", t.TSType, addressLength))
	}
	length := 8 + addressLength*2
	buf := make([]byte, length)
	buf[0] = t.TSType
	buf[1] = t.IPProtocol
	binary.BigEndian.PutUint16(buf[2:4], uint16(length))
	binary.BigEndian.PutUint16(buf[4:6], t.StartPort)
	binary.BigEndian.PutUint16(buf[6:8], t.EndPort)
	copy(buf[8:8+addressLength], t.StartAddr)
	copy(buf[8+addressLength:], t.EndAddr)
	return buf
}

func (t *TrafficSelector) syncOriginalFields() {
	if t.TSType == 0 {
		t.TSType = t.Type
	}
	if t.IPProtocol == 0 {
		t.IPProtocol = t.ProtocolID
	}
	t.Type = t.TSType
	t.ProtocolID = t.IPProtocol
}

func DecodePayloadTS(data []byte, initiator any) (*EncryptedPayloadTS, error) {
	isInitiator := initiatorFlag(initiator)
	if len(data) < 4 {
		return nil, errors.New("TS 载荷太短")
	}
	count := int(data[0])
	payloadType := TSR
	if isInitiator {
		payloadType = TSI
	}
	payload := &EncryptedPayloadTS{
		IsInitiator: isInitiator, PayloadType: payloadType, TSNumber: data[0],
		TrafficSelectors: make([]*TrafficSelector, 0, count),
	}
	offset := 4
	for range count {
		selector, consumed, err := decodeTrafficSelector(data[offset:])
		if err != nil {
			return nil, err
		}
		payload.TrafficSelectors = append(payload.TrafficSelectors, selector)
		offset += consumed
	}
	if offset != len(data) {
		return nil, fmt.Errorf("TS 载荷包含 %d 个尾随字节", len(data)-offset)
	}
	payload.Selectors = payload.TrafficSelectors
	return payload, nil
}

func initiatorFlag(argument any) bool {
	switch value := argument.(type) {
	case bool:
		return value
	case PayloadType:
		return value == TSI
	case uint8:
		return PayloadType(value) == TSI
	case int:
		return PayloadType(value) == TSI
	default:
		panic(fmt.Sprintf("TS initiator discriminator has unsupported type %T", argument))
	}
}

func decodeTrafficSelector(data []byte) (*TrafficSelector, int, error) {
	if len(data) < 8 {
		return nil, 0, errors.New("TS 载荷对于选择器头部来说太短")
	}
	length := int(binary.BigEndian.Uint16(data[2:4]))
	addressLength := 0
	switch data[0] {
	case TS_IPV4_ADDR_RANGE:
		addressLength = net.IPv4len
	case TS_IPV6_ADDR_RANGE:
		addressLength = net.IPv6len
	default:
		return nil, 0, errors.New("不支持的 TS 类型")
	}
	expected := 8 + addressLength*2
	if length != expected || length > len(data) {
		return nil, 0, errors.New("TS 载荷对于选择器主体来说太短")
	}
	selector := newTrafficSelector(
		data[0], data[1], data[8:8+addressLength], data[8+addressLength:length],
		binary.BigEndian.Uint16(data[4:6]), binary.BigEndian.Uint16(data[6:8]),
	)
	return selector, length, nil
}
