package smscodec

import (
	"errors"
	"fmt"
	"strings"

	smspdu "github.com/warthog618/sms"
	"github.com/warthog618/sms/encoding/tpdu"
	"github.com/warthog618/sms/encoding/ucs2"
)

type SMSEncoding string

const (
	SMSEncodingAuto SMSEncoding = "auto"
	SMSEncodingUCS2 SMSEncoding = "ucs2"
)

type SubmitOptions struct {
	Encoding        SMSEncoding
	ConcatReference int
}

type fixedConcatCounter int

func (c fixedConcatCounter) Count() int { return int(c) }

func NormalizeSMSEncoding(raw string) (SMSEncoding, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "", string(SMSEncodingAuto):
		return SMSEncodingAuto, nil
	case string(SMSEncodingUCS2):
		return SMSEncodingUCS2, nil
	default:
		return "", fmt.Errorf("unsupported SMS encoding: %s", raw)
	}
}

// BuildSubmitTPDUs 编码上行短信为一组 SUBMIT TPDU（支持长短信切片）。
// 返回 TPDU 字节数组列表 和 对应的长度列表（不含 SMSC），以及可能的错误。
func BuildSubmitTPDUs(to, text string) ([][]byte, []int, error) {
	return BuildSubmitTPDUsWithOptions(to, text, SubmitOptions{})
}

// BuildSubmitTPDUsWithOptions 编码上行短信为一组 SUBMIT TPDU，并允许调用方指定文本编码策略。
func BuildSubmitTPDUsWithOptions(to, text string, opts SubmitOptions) ([][]byte, []int, error) {
	normalizedTo := strings.TrimSpace(to)
	encoding, err := NormalizeSMSEncoding(string(opts.Encoding))
	if err != nil {
		return nil, nil, err
	}

	msg := []byte(text)
	encoderOptions := []smspdu.EncoderOption{smspdu.AsSubmit, smspdu.To(normalizedTo)}
	if encoding == SMSEncodingUCS2 {
		msg = ucs2.Encode([]rune(text))
		encoderOptions = append(encoderOptions, smspdu.AsUCS2)
	}

	encoder := smspdu.NewEncoder(encoderOptions...)
	if opts.ConcatReference != 0 {
		encoder.ConcatRef = fixedConcatCounter(opts.ConcatReference)
	}
	tpdus, err := encoder.Encode(msg)
	if err != nil {
		return nil, nil, err
	}
	if len(tpdus) == 0 {
		return nil, nil, errors.New("TPDU 编码结果为空")
	}

	bytesList := make([][]byte, 0, len(tpdus))
	lenList := make([]int, 0, len(tpdus))
	for _, pdu := range tpdus {
		if IsShortCode(normalizedTo) {
			da := pdu.DA
			da.SetTypeOfNumber(tpdu.TonUnknown)
			da.SetNumberingPlan(tpdu.NpISDN)
			pdu.DA = da
		}

		b, err := pdu.MarshalBinary()
		if err != nil {
			return nil, nil, err
		}
		bytesList = append(bytesList, b)
		lenList = append(lenList, len(b))
	}

	return bytesList, lenList, nil
}
