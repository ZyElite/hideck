package identity

import (
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
)

type apduResponse struct {
	body []byte
	sw1  byte
	sw2  byte
}

func (r apduResponse) success() bool {
	return r.sw1 == 0x90 && r.sw2 == 0x00
}

func (r apduResponse) status() string {
	return fmt.Sprintf("%02X%02X", r.sw1, r.sw2)
}

func transmitAPDU(access LogicalChannelAccess, channel int, command []byte) (apduResponse, error) {
	response, err := transmitAPDUOnce(access, channel, command)
	if err != nil {
		return apduResponse{}, err
	}
	if response.sw1 == 0x6C {
		retry := append([]byte(nil), command...)
		if len(retry) >= 5 {
			retry[len(retry)-1] = response.sw2
		} else {
			retry = append(retry, response.sw2)
		}
		return transmitAPDUOnce(access, channel, retry)
	}
	if response.sw1 != 0x61 {
		return response, nil
	}

	le := response.sw2
	followUp, err := transmitAPDUOnce(access, channel, []byte{0x00, 0xC0, 0x00, 0x00, le})
	if err != nil {
		return apduResponse{}, err
	}
	followUp.body = append(append([]byte(nil), response.body...), followUp.body...)
	return followUp, nil
}

func transmitAPDUOnce(access LogicalChannelAccess, channel int, command []byte) (apduResponse, error) {
	if access == nil {
		return apduResponse{}, errors.New("identity: no logical channel access")
	}
	commandHex := strings.ToUpper(hex.EncodeToString(command))
	responseHex, err := access.TransmitAPDU(channel, commandHex)
	if err != nil {
		return apduResponse{}, err
	}
	raw, err := hex.DecodeString(strings.TrimSpace(responseHex))
	if err != nil {
		return apduResponse{}, fmt.Errorf("identity: decode APDU response: %w", err)
	}
	if len(raw) < 2 {
		return apduResponse{}, fmt.Errorf("identity: APDU response too short: %d", len(raw))
	}
	return apduResponse{
		body: append([]byte(nil), raw[:len(raw)-2]...),
		sw1:  raw[len(raw)-2],
		sw2:  raw[len(raw)-1],
	}, nil
}

func readTransparentISIMEF(access LogicalChannelAccess, channel int, fileID uint16) ([]byte, error) {
	selected, err := selectISIMFile(access, channel, fileID)
	if err != nil {
		return nil, err
	}
	size := fileSizeFromFCP(selected.body)
	if size <= 0 {
		size = 256
	}

	result := make([]byte, 0, size)
	for offset := 0; offset < size; {
		chunk := min(size-offset, 256)
		response, err := transmitAPDU(access, channel, readBinaryAPDU(offset, chunk))
		if err != nil {
			return nil, err
		}
		if !response.success() {
			return nil, fmt.Errorf("READ BINARY %04X offset=%d failed: SW=%s", fileID, offset, response.status())
		}
		result = append(result, response.body...)
		if len(response.body) == 0 || size == 256 && len(response.body) < chunk {
			break
		}
		offset += len(response.body)
	}
	return result, nil
}

func readLinearFixedISIMEF(access LogicalChannelAccess, channel int, fileID uint16, maxRecords int) ([][]byte, error) {
	selected, err := selectISIMFile(access, channel, fileID)
	if err != nil {
		return nil, err
	}
	if maxRecords <= 0 {
		maxRecords = 16
	}
	recordLength, recordCount := recordInfoFromFCP(selected.body)
	if recordCount > 0 && recordCount < maxRecords {
		maxRecords = recordCount
	}
	if recordLength <= 0 {
		recordLength = 256
	}

	records := make([][]byte, 0, maxRecords)
	for record := 1; record <= maxRecords; record++ {
		response, err := transmitAPDU(access, channel, readRecordAPDU(record, recordLength))
		if err != nil {
			return nil, err
		}
		if isRecordNotFound(response.sw1, response.sw2) {
			break
		}
		if !response.success() {
			return nil, fmt.Errorf("READ RECORD %04X #%d failed: SW=%s", fileID, record, response.status())
		}
		records = append(records, append([]byte(nil), response.body...))
	}
	return records, nil
}

func selectISIMFile(access LogicalChannelAccess, channel int, fileID uint16) (apduResponse, error) {
	command := []byte{0x00, 0xA4, 0x00, 0x04, 0x02, byte(fileID >> 8), byte(fileID)}
	response, err := transmitAPDU(access, channel, command)
	if err != nil {
		return apduResponse{}, err
	}
	if !response.success() {
		return response, fmt.Errorf("SELECT %04X failed: SW=%s", fileID, response.status())
	}
	return response, nil
}

func readBinaryAPDU(offset, length int) []byte {
	if length <= 0 || length > 256 {
		length = 256
	}
	return []byte{0x00, 0xB0, byte(offset >> 8), byte(offset), apduLengthByte(length)}
}

func readRecordAPDU(record, length int) []byte {
	if length <= 0 || length > 256 {
		length = 256
	}
	return []byte{0x00, 0xB2, byte(record), 0x04, apduLengthByte(length)}
}

func apduLengthByte(length int) byte {
	if length == 256 {
		return 0
	}
	return byte(length)
}

func isRecordNotFound(sw1, sw2 byte) bool {
	return sw1 == 0x6A && (sw2 == 0x82 || sw2 == 0x83)
}
