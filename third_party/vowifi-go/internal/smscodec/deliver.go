package smscodec

import (
	"strings"
	"time"

	smspdu "github.com/warthog618/sms"
	"github.com/warthog618/sms/encoding/tpdu"
)

// ConcatInfo 长短信分片信息（UDH concatenation header）
type ConcatInfo struct {
	IsConcat bool // 是否为多段短信
	Ref      int  // 引用号（同一条长短信的所有分片共享此值）
	RefBits  int  // 引用号位宽：8 或 16
	Total    int  // 总分片数
	Seq      int  // 当前序号 (1-based)
}

// DecodeDeliverTPDU 解码下行短信 TPDU，返回发送方号码、文本内容、发送时间、和 concat 分片信息。
// 如果 TPDU 包含 UDH concatenation header（长短信分片），concat.IsConcat 为 true。
func DecodeDeliverTPDU(tpduBytes []byte) (sender string, text string, ts time.Time, concat ConcatInfo, err error) {
	if trimmed, ok := TrimDeliverTPDUToDeclaredLength(tpduBytes); ok {
		tpduBytes = trimmed
	}
	if normalized, ok := normalizeDeliverTPDUGSM7SpareBits(tpduBytes); ok {
		tpduBytes = normalized
	}
	t, err := smspdu.Unmarshal(tpduBytes)
	if err != nil {
		return "", "", time.Time{}, ConcatInfo{}, err
	}
	msg, err := smspdu.Decode([]*tpdu.TPDU{t})
	if err != nil {
		return "", "", time.Time{}, ConcatInfo{}, err
	}
	if t.UDH != nil {
		if segments, seqno, mref, ok := t.UDH.ConcatInfo8(); ok && segments > 1 {
			concat = ConcatInfo{IsConcat: true, Ref: mref, RefBits: 8, Total: segments, Seq: seqno}
		} else if segments, seqno, mref, ok := t.UDH.ConcatInfo16(); ok && segments > 1 {
			concat = ConcatInfo{IsConcat: true, Ref: mref, RefBits: 16, Total: segments, Seq: seqno}
		}
	}

	textStr := string(msg)
	alpha, aErr := t.DCS.Alphabet()
	if aErr == nil && alpha == tpdu.Alpha8Bit {
		classified := classifyBinarySMS(t, msg)
		textStr = formatBinaryClassification(classified)
	}
	textStr = strings.ToValidUTF8(textStr, "")

	if t.SmsType() == tpdu.SmsDeliver {
		return t.OA.Number(), textStr, t.SCTS.Time, concat, nil
	}
	return "", textStr, time.Time{}, concat, nil
}

// IsShortCode 判断号码是否为运营商短号码/服务号码（非标准手机号）
// 短号码特征：无 + 前缀、长度 <= 6 位、纯数字
func IsShortCode(phone string) bool {
	if strings.HasPrefix(phone, "+") {
		return false
	}
	digits := strings.TrimLeft(phone, "0123456789")
	return digits == "" && len(phone) <= 6
}
