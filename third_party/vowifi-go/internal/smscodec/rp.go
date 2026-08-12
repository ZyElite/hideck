package smscodec

import (
	"errors"
	"fmt"
	"strings"
)

type RPDUKind string

const (
	RPDUKindUnknown RPDUKind = "UNKNOWN"
	RPDUKindData    RPDUKind = "RP-DATA"
	RPDUKindAck     RPDUKind = "RP-ACK"
	RPDUKindError   RPDUKind = "RP-ERROR"
)

type RPDUInfo struct {
	Kind    RPDUKind
	RawType byte
	MR      byte
	Cause   int
}

type RPErrorDetails struct {
	MR          byte
	Cause       byte
	Diagnostics []byte
	UserData    []byte
}

const rpUserDataIEI byte = 0x41

// ParseRPData 解析 RP-DATA（RPDU）并提取 RP-MR 与 TPDU。
func ParseRPData(body []byte) (byte, []byte, error) {
	if len(body) < 3 {
		return 0, nil, fmt.Errorf("RPDU 过短")
	}
	i := 0
	i++
	rpMR := body[i]
	i++
	if i >= len(body) {
		return 0, nil, fmt.Errorf("RP-DA 缺失")
	}
	daLen := int(body[i])
	i++
	if i+daLen > len(body) {
		return 0, nil, fmt.Errorf("RP-DA 超界")
	}
	i += daLen

	if i >= len(body) {
		return 0, nil, fmt.Errorf("RP-OA 缺失")
	}
	oaLen := int(body[i])
	i++
	if i+oaLen > len(body) {
		return 0, nil, fmt.Errorf("RP-OA 超界")
	}
	i += oaLen

	if i >= len(body) {
		return 0, nil, fmt.Errorf("RP-UD 缺失")
	}
	udLen := int(body[i])
	i++
	if i+udLen > len(body) {
		return 0, nil, fmt.Errorf("RP-UD 超界")
	}
	tpduBytes := body[i : i+udLen]
	return rpMR, tpduBytes, nil
}

func ClassifyRPDU(body []byte) RPDUInfo {
	if len(body) == 0 {
		return RPDUInfo{Kind: RPDUKindUnknown}
	}
	info := RPDUInfo{RawType: body[0], Kind: RPDUKindUnknown}
	if len(body) > 1 {
		info.MR = body[1]
	}
	switch body[0] {
	case 0x00, 0x01:
		info.Kind = RPDUKindData
	case 0x02, 0x03:
		info.Kind = RPDUKindAck
	case 0x04, 0x05:
		info.Kind = RPDUKindError
		if cause, err := ParseRPErrorCause(body); err == nil {
			info.Cause = int(cause)
		}
	}
	return info
}

// ParseRPErrorCause 解析 RP-ERROR cause（支持可变长度 Cause IE）。
func ParseRPErrorCause(body []byte) (byte, error) {
	details, _, err := parseRPErrorCauseDetails(body)
	if err != nil {
		return 0, err
	}
	return details.Cause, nil
}

// ParseRPErrorDetails decodes the mandatory RP-Cause and optional RP-User-Data.
func ParseRPErrorDetails(body []byte) (RPErrorDetails, error) {
	details, offset, err := parseRPErrorCauseDetails(body)
	if err != nil {
		return RPErrorDetails{}, err
	}
	userData, err := parseOptionalRPErrorUserData(body[offset:])
	if err != nil {
		return RPErrorDetails{}, err
	}
	details.UserData = userData
	return details, nil
}

func parseRPErrorCauseDetails(body []byte) (RPErrorDetails, int, error) {
	if len(body) < 4 {
		return RPErrorDetails{}, 0, fmt.Errorf("RP-ERROR 长度不足")
	}
	if body[0] != 0x04 && body[0] != 0x05 {
		return RPErrorDetails{}, 0, fmt.Errorf("非 RP-ERROR: mti=0x%02x", body[0])
	}
	causeIELen := int(body[2])
	if causeIELen <= 0 {
		return RPErrorDetails{}, 0, fmt.Errorf("RP-ERROR cause IE 为空")
	}
	if 3+causeIELen > len(body) {
		return RPErrorDetails{}, 0, fmt.Errorf("RP-ERROR cause IE 越界")
	}
	// 3GPP TS 24.011 cause 为首字节低 7 位，后续诊断字节按需忽略。
	details := RPErrorDetails{
		MR: body[1], Cause: body[3] & 0x7F,
		Diagnostics: append([]byte(nil), body[4:3+causeIELen]...),
	}
	offset := 3 + causeIELen
	return details, offset, nil
}

func parseOptionalRPErrorUserData(data []byte) ([]byte, error) {
	if len(data) == 0 || (len(data) == 1 && data[0] == 0) {
		return nil, nil
	}
	if data[0] != rpUserDataIEI {
		return nil, fmt.Errorf("RP-ERROR user data IEI 非法: 0x%02x", data[0])
	}
	if len(data) < 2 {
		return nil, fmt.Errorf("RP-ERROR user data IE 长度缺失")
	}
	userDataLength := int(data[1])
	if userDataLength != len(data)-2 {
		return nil, fmt.Errorf("RP-ERROR user data IE 长度不匹配")
	}
	return append([]byte(nil), data[2:]...), nil
}

func ParseRPDataWithAddresses(body []byte) (byte, string, string, []byte, error) {
	if len(body) < 5 {
		return 0, "", "", nil, fmt.Errorf("RPDU 过短")
	}
	i := 0
	i++
	rpMR := body[i]
	i++

	var oa, da string
	if i >= len(body) {
		return 0, "", "", nil, fmt.Errorf("RP-OA 缺失")
	}
	oaLen := int(body[i])
	i++
	if i+oaLen > len(body) {
		return 0, "", "", nil, fmt.Errorf("RP-OA 超界")
	}
	if oaLen > 0 {
		oa, _ = DecodeAddressValue(body[i : i+oaLen])
	}
	i += oaLen

	if i >= len(body) {
		return 0, oa, "", nil, fmt.Errorf("RP-DA 缺失")
	}
	daLen := int(body[i])
	i++
	if i+daLen > len(body) {
		return 0, oa, "", nil, fmt.Errorf("RP-DA 超界")
	}
	if daLen > 0 {
		da, _ = DecodeAddressValue(body[i : i+daLen])
	}
	i += daLen

	if i >= len(body) {
		return 0, oa, da, nil, fmt.Errorf("RP-UD 缺失")
	}
	udLen := int(body[i])
	i++
	if i+udLen > len(body) {
		return 0, oa, da, nil, fmt.Errorf("RP-UD 超界")
	}
	tpduBytes := body[i : i+udLen]
	return rpMR, oa, da, tpduBytes, nil
}

func DecodeAddressValue(v []byte) (string, error) {
	if len(v) < 1 {
		return "", errors.New("address value 为空")
	}
	ton := v[0]
	bcd := v[1:]

	prefix := ""
	if ton&0x70 == 0x10 {
		prefix = "+"
	}

	var sb strings.Builder
	sb.WriteString(prefix)
	for _, b := range bcd {
		lo := b & 0x0F
		hi := (b >> 4) & 0x0F
		if lo <= 9 {
			sb.WriteByte('0' + lo)
		} else if lo == 0x0F {
		} else {
			return "", fmt.Errorf("非法 BCD digit: %x", lo)
		}
		if hi <= 9 {
			sb.WriteByte('0' + hi)
		} else if hi == 0x0F {
		} else {
			return "", fmt.Errorf("非法 BCD digit: %x", hi)
		}
	}
	return sb.String(), nil
}

// EncodeAddress 将号码编码为 LV 格式的 RP-Address（Length + Type + BCD）。
// Length 是 Value 部分（Type + BCD）的字节数。
func EncodeAddress(number string) []byte {
	number = strings.TrimSpace(number)
	if number == "" {
		return []byte{0x00}
	}

	ton := byte(0x81) // Unknown, ISDN/telephone numbering plan
	if strings.HasPrefix(number, "+") {
		ton = 0x91 // International, ISDN/telephone numbering plan
		number = number[1:]
	}

	// BCD 编码：每两个数字一组，低位在前，高位在后
	// 如果是奇数个数字，最后补 F
	length := len(number)
	bcdLen := (length + 1) / 2
	bcd := make([]byte, bcdLen)
	for i := 0; i < length; i++ {
		digit := byte(number[i] - '0')
		if i%2 == 0 {
			bcd[i/2] |= digit
		} else {
			bcd[i/2] |= digit << 4
		}
	}
	if length%2 != 0 {
		bcd[length/2] |= 0xF0
	}

	// RP-Address Value 部分 = Type (1 byte) + BCD
	// RP-Address IE = Length (1 byte) + Value
	totalLen := 1 + len(bcd)
	out := make([]byte, 1+totalLen)
	out[0] = byte(totalLen)
	out[1] = ton
	copy(out[2:], bcd)
	return out
}

// BuildRPData 构造 RP-DATA（RPDU），携带指定 RP-MR 与 TPDU。
func BuildRPData(rpMR byte, tpduBytes []byte, smsc string) []byte {
	smscAddr := EncodeAddress(smsc)

	out := make([]byte, 0, 2+1+len(smscAddr)+1+len(tpduBytes))
	out = append(out, 0x00) // RP-Message Type: RP-DATA (MS -> Network)
	out = append(out, rpMR) // RP-Message Reference
	out = append(out, 0x00) // RP-Originator Address Length = 0
	out = append(out, smscAddr...)
	out = append(out, byte(len(tpduBytes)))
	out = append(out, tpduBytes...)
	return out
}

// BuildRPAck 构造 RP-ACK（确认收到 RP-DATA）。
func BuildRPAck(rpMR byte) []byte {
	return []byte{0x02, rpMR}
}

// RPCauseTemporaryFailure 临时故障（3GPP TS 24.011 §8.2.5.4），SMSC 应稍后重试
const RPCauseTemporaryFailure byte = 41

// BuildRPError 构造 RP-ERROR（拒收 RP-DATA，通知 SMSC 投递失败）。
func BuildRPError(rpMR byte, cause byte) []byte {
	return []byte{0x04, rpMR, 0x01, cause, 0x00}
}
