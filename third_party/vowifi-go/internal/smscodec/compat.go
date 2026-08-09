package smscodec

import (
	"fmt"
	"time"

	smspdu "github.com/warthog618/sms"
	"github.com/warthog618/sms/encoding/tpdu"
)

// SetSubmitMessageReference replaces TP-MR on an encoded SMS-SUBMIT TPDU.
func SetSubmitMessageReference(encoded []byte, reference byte) ([]byte, error) {
	message := tpdu.TPDU{Direction: tpdu.MO}
	if err := message.UnmarshalBinary(encoded); err != nil {
		return nil, fmt.Errorf("smscodec: decode SMS-SUBMIT: %w", err)
	}
	if message.SmsType() != tpdu.SmsSubmit {
		return nil, fmt.Errorf("smscodec: TPDU is not SMS-SUBMIT")
	}
	message.MR = reference
	result, err := message.MarshalBinary()
	if err != nil {
		return nil, fmt.Errorf("smscodec: encode SMS-SUBMIT: %w", err)
	}
	return result, nil
}

// DecodedMessage preserves the structured result added by the interim codec.
type DecodedMessage struct {
	Sender          string
	Text            string
	Timestamp       time.Time
	ConcatReference int
	ConcatRefBits   int
	TotalParts      int
	PartNo          int
	Err             error
}

// DecodeDeliverTPDUMessage projects the recovered tuple API into the interim
// structured result.
func DecodeDeliverTPDUMessage(pdu []byte) DecodedMessage {
	sender, text, timestamp, concat, err := DecodeDeliverTPDU(pdu)
	if err != nil {
		return DecodedMessage{Err: err}
	}
	sender = applyInterimSenderPrefix(pdu, sender)
	message := DecodedMessage{
		Sender: sender, Text: text, Timestamp: timestamp,
		TotalParts: 1, PartNo: 1,
	}
	if concat.IsConcat {
		message.ConcatReference = concat.Ref
		message.ConcatRefBits = concat.RefBits
		message.TotalParts = concat.Total
		message.PartNo = concat.Seq
	}
	return message
}

func applyInterimSenderPrefix(pdu []byte, sender string) string {
	if trimmed, ok := TrimDeliverTPDUToDeclaredLength(pdu); ok {
		pdu = trimmed
	}
	if normalized, ok := normalizeDeliverTPDUGSM7SpareBits(pdu); ok {
		pdu = normalized
	}
	message, err := smspdu.Unmarshal(pdu)
	if err == nil && message.PID>>4&7 == 1 {
		return "+" + sender
	}
	return sender
}

// TrimDeliverTPDUBytes preserves the interim slice-only projection.
func TrimDeliverTPDUBytes(pdu []byte) []byte {
	trimmed, _ := TrimDeliverTPDUToDeclaredLength(pdu)
	return trimmed
}

// BuildRPDataWithAddresses retains the interim general-address RP-DATA
// builder while BuildRPData keeps the recovered mobile-originated signature.
func BuildRPDataWithAddresses(mr byte, oa, da string, pdu []byte) []byte {
	oaEncoded := EncodeAddress(oa)
	daEncoded := EncodeAddress(da)
	result := make([]byte, 0, 3+len(oaEncoded)+len(daEncoded)+len(pdu))
	result = append(result, 0x00, mr)
	result = append(result, oaEncoded...)
	result = append(result, daEncoded...)
	result = append(result, byte(len(pdu)))
	return append(result, pdu...)
}

// BuildSubmitTPDUObjectsWithOptions preserves the interim object projection.
func BuildSubmitTPDUObjectsWithOptions(dest, text string, opts SubmitOptions) ([]tpdu.TPDU, error) {
	encoded, _, err := BuildSubmitTPDUsWithOptions(dest, text, opts)
	if err != nil {
		return nil, err
	}
	result := make([]tpdu.TPDU, len(encoded))
	for index := range encoded {
		result[index].Direction = tpdu.MO
		if err := result[index].UnmarshalBinary(encoded[index]); err != nil {
			return nil, err
		}
	}
	return result, nil
}
