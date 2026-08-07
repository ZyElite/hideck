// Package smscodec encodes and decodes SMS PDUs.
//
// This is a clean-room style reconstruction of the vohive pkg/smscodec package,
// recovered from the stripped Go 1.26 binary. The original delegates the actual
// PDU parsing/encoding to the public github.com/warthog618/sms library, and adds
// modem-specific hardening on top:
//
//   - trimming PDUs to their declared length (some modems pad or truncate),
//   - normalizing GSM-7 spare bits in the last user-data byte,
//   - classifying binary SMS (WSP push / OMA provisioning / SIM OTA) and
//     producing a human-readable summary,
//   - multi-part (concatenated) SMS building for long messages.
//
// Recovery notes (from decompilation, function name -> logic):
//   - NormalizeSMSEncoding: accepts only "auto" / "ucs2" (case-insensitive).
//   - IsShortCode: "+" prefixed or ≥7 digit numbers are not short codes.
//   - DecodeDeliverTPDU: TrimDeliverTPDUToDeclaredLength ->
//     normalizeDeliverTPDUGSM7SpareBits -> sms.Unmarshal -> binary classification.
package smscodec

import (
	"fmt"
	"strings"
	"time"

	"github.com/warthog618/sms"
	"github.com/warthog618/sms/encoding/tpdu"
)

// NormalizeSMSEncoding validates and normalizes a user-supplied SMS encoding
// name. Only "auto" and "ucs2" are accepted; the value is compared
// case-insensitively after trimming surrounding whitespace.
func NormalizeSMSEncoding(enc string) (string, error) {
	enc = strings.TrimSpace(strings.ToLower(enc))
	switch enc {
	case "":
		return "auto", nil
	case "auto", "ucs2":
		return enc, nil
	default:
		return "", fmt.Errorf("unsupported SMS encoding %q", enc)
	}
}

// IsShortCode reports whether number looks like a short code rather than a
// full phone number: it must not be "+" prefixed and, after stripping all
// digits, nothing else may remain — with fewer than 7 digits total.
func IsShortCode(number string) bool {
	if len(number) != 0 && number[0] == '+' {
		return false
	}
	rest := strings.TrimLeft(number, "0123456789")
	return len(rest) == 0 && len(number) < 7
}

// DecodedMessage is the result of decoding an SMS-DELIVER TPDU.
type DecodedMessage struct {
	Sender          string    // TP-OA originating address (with "+" for international, see note)
	Text            string    // message text, UTF-8 sanitized (strings.ToValidUTF8)
	Timestamp       time.Time // TP-SCTS service centre time stamp
	ConcatReference int       // TP-UDH concatenation reference, zero for a single-part message
	ConcatRefBits   int       // concatenation reference width: 8 or 16 bits
	TotalParts      int       // number of concatenated parts, one for a single-part message
	PartNo          int       // one-based concatenated part number
	Err             error     // non-nil when the PDU could not be decoded
}

// DecodeDeliverTPDU decodes a hex SMS-DELIVER PDU into a DecodedMessage.
//
// The original first trims the PDU to its declared length and normalizes the
// GSM-7 spare bits (some modems corrupt these), then hands the result to
// github.com/warthog618/sms for the actual parsing. Binary SMS (5GSE) is
// classified and replaced with a human-readable summary (WSP push / OMA CP /
// SIM OTA); the classification step is implemented in classifier.go.
func DecodeDeliverTPDU(pdu []byte) DecodedMessage {
	pdu = TrimDeliverTPDUToDeclaredLength(pdu)
	pdu = normalizeDeliverTPDUGSM7SpareBits(pdu)

	t, err := sms.Unmarshal(pdu)
	if err != nil {
		return DecodedMessage{Err: err}
	}

	msg := DecodedMessage{
		Sender:     t.OA.Number(),
		Timestamp:  t.SCTS.Time,
		TotalParts: 1,
		PartNo:     1,
	}
	if total, partNo, reference, ok := t.ConcatInfo(); ok {
		msg.ConcatReference = reference
		msg.TotalParts = total
		msg.PartNo = partNo
		msg.ConcatRefBits = 16
		if _, _, _, is8Bit := t.UDH.ConcatInfo8(); is8Bit {
			msg.ConcatRefBits = 8
		}
	}

	// 3GPP TS 23.040 9.2.3.9: TP-PID bits 6..4 == 1 ("no interworking,
	// short message type") selects the short message type; the original adds
	// the international "+" prefix for these (E.164) addresses.
	if t.PID>>4&7 == 1 {
		msg.Sender = "+" + msg.Sender
	}

	text, err := sms.Decode([]*tpdu.TPDU{t})
	if err != nil {
		msg.Err = err
		return msg
	}
	msg.Text = strings.ToValidUTF8(string(text), "")

	// Binary SMS (DCS 5GSE / 8-bit data) is often a WAP/OMA push; replace the
	// raw text with a readable classification.
	if classification, ok := classifyBinarySMS(t); ok {
		msg.Text = classification
	}

	return msg
}
