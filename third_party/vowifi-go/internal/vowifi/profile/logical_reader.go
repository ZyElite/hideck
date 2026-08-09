package profile

import (
	"encoding/hex"
	"errors"
	"fmt"
	"strings"

	"github.com/iniwex5/vowifi-go/internal/vowifi/apdu"
)

const (
	isimAID                 = "A0000000871004"
	efIMPI           uint16 = 0x6F02
	efDomain         uint16 = 0x6F03
	efIMPU           uint16 = 0x6F04
	fallbackEFLength        = 0x80
	maxIMPURecords          = 0x10
	maxAPDUFollowUps        = 3
)

type LogicalChannelTransport = apdu.LogicalChannelTransport
type LogicalChannelAIDResolver = apdu.LogicalChannelAIDResolver

func ReadISIMIdentity(transport LogicalChannelTransport) (Identity, error) {
	if transport == nil {
		return Identity{}, errors.New("ISIM APDU transport 为空")
	}
	channel, err := transport.OpenLogicalChannel(resolveISIMAID(transport))
	if err != nil {
		return Identity{}, fmt.Errorf("打开 ISIM 逻辑通道失败: %w", err)
	}
	defer func() { _ = transport.CloseLogicalChannel(channel) }()

	var identity Identity
	if raw, readErr := readTransparentEF(transport, channel, efIMPI, fallbackEFLength); readErr == nil {
		values := decodeIdentityValues(raw)
		if len(values) > 0 {
			identity.IMPI = values[0]
		}
	}
	if raw, readErr := readTransparentEF(transport, channel, efDomain, fallbackEFLength); readErr == nil {
		values := decodeIdentityValues(raw)
		if len(values) > 0 {
			identity.Domain = values[0]
		}
	}
	if records, readErr := readLinearFixedRecords(
		transport, channel, efIMPU, fallbackEFLength, maxIMPURecords,
	); readErr == nil {
		for _, record := range records {
			for _, value := range decodeIdentityValues(record) {
				identity.IMPU = appendUnique(identity.IMPU, value)
			}
		}
	}
	if identity.IMPI == "" && identity.Domain == "" && len(identity.IMPU) == 0 {
		return Identity{}, errors.New("ISIM 未读取到 IMPI/IMPU/DOMAIN")
	}
	return identity, nil
}

func resolveISIMAID(transport LogicalChannelTransport) string {
	resolver, ok := transport.(LogicalChannelAIDResolver)
	if !ok {
		return isimAID
	}
	resolved, _, err := resolver.ResolveLogicalChannelAID("isim", isimAID)
	if err != nil {
		return isimAID
	}
	resolved = strings.ToUpper(strings.TrimSpace(resolved))
	if len(resolved) < len(isimAID) || !strings.HasPrefix(resolved, isimAID) {
		return isimAID
	}
	return resolved
}

func readTransparentEF(
	transport LogicalChannelTransport,
	channel int,
	efID uint16,
	fallbackLength int,
) ([]byte, error) {
	selected, err := transmitLogicalWithFollowUp(transport, channel, selectFileAPDU(efID))
	if err != nil {
		return nil, err
	}
	fcp, err := extractSuccessData(selected)
	if err != nil {
		return nil, err
	}
	length := parseTransparentFileSizeFromFCP(fcp)
	if length < 1 || length > 0xFF {
		length = fallbackLength
	}
	response, err := transmitLogicalWithFollowUp(
		transport, channel, []byte{0x00, 0xB0, 0x00, 0x00, byte(length)},
	)
	if err != nil {
		return nil, err
	}
	return extractSuccessData(response)
}

func readLinearFixedRecords(
	transport LogicalChannelTransport,
	channel int,
	efID uint16,
	fallbackLength int,
	maxRecords int,
) ([][]byte, error) {
	selected, err := transmitLogicalWithFollowUp(transport, channel, selectFileAPDU(efID))
	if err != nil {
		return nil, err
	}
	fcp, err := extractSuccessData(selected)
	if err != nil {
		return nil, err
	}
	recordLength, recordCount := parseLinearFixedMetaFromFCP(fcp)
	if recordLength < 1 || recordLength > 0xFF {
		recordLength = fallbackLength
	}
	if recordCount < 1 || recordCount >= maxRecords {
		recordCount = maxRecords
	}
	return readRecords(transport, channel, recordLength, recordCount)
}

func readRecords(
	transport LogicalChannelTransport,
	channel int,
	recordLength int,
	recordCount int,
) ([][]byte, error) {
	records := make([][]byte, 0, recordCount)
	var lastErr error
	for record := 1; record <= recordCount; record++ {
		command := []byte{0x00, 0xB2, byte(record), 0x04, byte(recordLength)}
		response, err := transmitLogicalWithFollowUp(transport, channel, command)
		if err != nil {
			lastErr = err
			continue
		}
		sw1, sw2, ok := apduStatus(response)
		if !ok {
			lastErr = fmt.Errorf("记录 %d 响应过短: %X", record, response)
			continue
		}
		if sw1 == 0x6A && (sw2 == 0x82 || sw2 == 0x83) {
			break
		}
		if !isAPDUSuccess(sw1) {
			lastErr = fmt.Errorf("记录 %d SW=%02X%02X", record, sw1, sw2)
			continue
		}
		records = append(records, append([]byte(nil), response[:len(response)-2]...))
	}
	if len(records) == 0 && lastErr != nil {
		return nil, lastErr
	}
	return records, nil
}

func selectFileAPDU(efID uint16) []byte {
	return []byte{0x00, 0xA4, 0x00, 0x04, 0x02, byte(efID >> 8), byte(efID)}
}

func transmitLogicalWithFollowUp(
	transport LogicalChannelTransport,
	channel int,
	command []byte,
) ([]byte, error) {
	transmitString := func(encoded string) (string, error) {
		return transport.TransmitAPDU(channel, encoded)
	}
	transmit := func(next []byte) ([]byte, error) {
		return transmitHex(transmitString, next)
	}
	response, err := transmit(command)
	if err != nil {
		return nil, err
	}
	return followUpIfNeededWithLimit(transmit, command, response, maxAPDUFollowUps)
}

func transmitHex(transmit func(string) (string, error), command []byte) ([]byte, error) {
	response, err := transmit(strings.ToUpper(hex.EncodeToString(command)))
	if err != nil {
		return nil, err
	}
	decoded, err := hex.DecodeString(strings.TrimSpace(response))
	if err != nil {
		return nil, fmt.Errorf("APDU HEX 解码失败: %w", err)
	}
	return decoded, nil
}

func followUpIfNeeded(
	transmit func([]byte) ([]byte, error),
	command []byte,
	response []byte,
) ([]byte, error) {
	return followUpIfNeededWithLimit(transmit, command, response, maxAPDUFollowUps)
}

func followUpIfNeededWithLimit(
	transmit func([]byte) ([]byte, error),
	command []byte,
	response []byte,
	limit int,
) ([]byte, error) {
	sw1, sw2, ok := apduStatus(response)
	if !ok {
		return nil, fmt.Errorf("APDU 响应过短: %X", response)
	}
	if limit < 1 {
		return response, nil
	}
	var next []byte
	switch sw1 {
	case 0x61:
		next = []byte{0x00, 0xC0, 0x00, 0x00, sw2}
	case 0x6C:
		if len(command) < 5 {
			return nil, fmt.Errorf("APDU 收到 6C%02X 但原命令无 Le: %X", sw2, command)
		}
		next = append([]byte(nil), command...)
		next[len(next)-1] = sw2
	default:
		return response, nil
	}
	nextResponse, err := transmit(next)
	if err != nil {
		return nil, err
	}
	return followUpIfNeededWithLimit(transmit, next, nextResponse, limit-1)
}

func apduStatus(response []byte) (byte, byte, bool) {
	if len(response) < 2 {
		return 0, 0, false
	}
	return response[len(response)-2], response[len(response)-1], true
}

func isAPDUSuccess(sw1 byte) bool {
	return sw1 == 0x90 || sw1 == 0x62 || sw1 == 0x63
}

func extractSuccessData(response []byte) ([]byte, error) {
	sw1, sw2, ok := apduStatus(response)
	if !ok {
		return nil, fmt.Errorf("APDU 响应过短: %X", response)
	}
	if !isAPDUSuccess(sw1) {
		return nil, fmt.Errorf("SW=%02X%02X", sw1, sw2)
	}
	return response[:len(response)-2], nil
}
