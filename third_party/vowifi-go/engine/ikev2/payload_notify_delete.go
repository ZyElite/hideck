package ikev2

import (
	"encoding/binary"
	"errors"
	"fmt"
)

type EncryptedPayloadNotify struct {
	ProtocolID ProtocolID
	SPI        []byte
	NotifyType uint16
	NotifyData []byte

	SPISize uint8
}

func (p *EncryptedPayloadNotify) Type() PayloadType { return N }

func (p *EncryptedPayloadNotify) Encode() ([]byte, error) {
	if p.SPISize != 0 && int(p.SPISize) != len(p.SPI) {
		return nil, fmt.Errorf("通知载荷 SPI 长度为 %d，声明为 %d", len(p.SPI), p.SPISize)
	}
	buf := make([]byte, 4+len(p.SPI)+len(p.NotifyData))
	buf[0] = uint8(p.ProtocolID)
	buf[1] = uint8(len(p.SPI))
	binary.BigEndian.PutUint16(buf[2:4], p.NotifyType)
	copy(buf[4:], p.SPI)
	copy(buf[4+len(p.SPI):], p.NotifyData)
	return buf, nil
}

func DecodePayloadNotify(data []byte) (*EncryptedPayloadNotify, error) {
	if len(data) < 4 {
		return nil, errors.New("通知载荷太短")
	}
	spiSize := int(data[1])
	if len(data) < 4+spiSize {
		return nil, errors.New("通知载荷对于 SPI 来说太短")
	}
	return &EncryptedPayloadNotify{
		ProtocolID: ProtocolID(data[0]), SPI: data[4 : 4+spiSize],
		NotifyType: binary.BigEndian.Uint16(data[2:4]), NotifyData: data[4+spiSize:],
		SPISize: uint8(spiSize),
	}, nil
}

type EncryptedPayloadDelete struct {
	ProtocolID ProtocolID
	SPISize    uint8
	NumSPIs    uint16
	SPIs       []byte
}

func (p *EncryptedPayloadDelete) Type() PayloadType { return D }

func (p *EncryptedPayloadDelete) Encode() ([]byte, error) {
	expected := int(p.SPISize) * int(p.NumSPIs)
	if expected != len(p.SPIs) {
		return nil, fmt.Errorf("删除载荷 SPI 数据长度为 %d，期望 %d", len(p.SPIs), expected)
	}
	buf := make([]byte, 4+len(p.SPIs))
	buf[0] = uint8(p.ProtocolID)
	buf[1] = p.SPISize
	binary.BigEndian.PutUint16(buf[2:4], p.NumSPIs)
	copy(buf[4:], p.SPIs)
	return buf, nil
}

func DecodePayloadDelete(data []byte) (*EncryptedPayloadDelete, error) {
	if len(data) < 4 {
		return nil, errors.New("删除载荷太短")
	}
	spiSize := data[1]
	numSPIs := binary.BigEndian.Uint16(data[2:4])
	expected := 4 + int(spiSize)*int(numSPIs)
	if len(data) < expected {
		return nil, errors.New("删除载荷对于 SPI 数据来说太短")
	}
	if len(data) != expected {
		return nil, fmt.Errorf("删除载荷包含 %d 个尾随字节", len(data)-expected)
	}
	return &EncryptedPayloadDelete{
		ProtocolID: ProtocolID(data[0]), SPISize: spiSize, NumSPIs: numSPIs, SPIs: data[4:expected],
	}, nil
}
