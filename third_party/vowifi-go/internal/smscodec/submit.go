package smscodec

import (
	"errors"
	"strings"

	"github.com/warthog618/sms"
	"github.com/warthog618/sms/encoding/tpdu"
	"github.com/warthog618/sms/encoding/ucs2"
)

// SubmitOptions controls how BuildSubmitTPDUsWithOptions encodes a message.
type SubmitOptions struct {
	// Encoding is the SMS alphabet to use: "auto" (default, GSM-7 when the
	// text fits, else UCS-2) or "ucs2". See NormalizeSMSEncoding.
	Encoding string
}

// BuildSubmitTPDUsWithOptions builds one or more SMS-SUBMIT TPDUs for a
// message, splitting it into a concatenated (multipart) series when it does
// not fit in a single user data payload (140 octets).
//
// The original: normalizes the requested encoding, then hands the work to
// github.com/warthog618/sms — sms.To(dest) fixes the destination address on
// the submit template and sms.Encode performs the (segmented) encoding.
func BuildSubmitTPDUsWithOptions(dest, text string, opts SubmitOptions) ([]tpdu.TPDU, error) {
	enc, err := NormalizeSMSEncoding(opts.Encoding)
	if err != nil {
		return nil, err
	}
	dest = strings.TrimSpace(dest)

	options := []sms.EncoderOption{sms.To(dest)}
	message := []byte(text)
	if enc == "ucs2" {
		message = ucs2.Encode([]rune(text))
		options = append(options, sms.AsUCS2)
	}

	parts, err := sms.Encode(message, options...)
	if err != nil {
		return nil, err
	}
	if len(parts) == 0 {
		return nil, errors.New("smscodec: TPDU encoding returned no parts")
	}
	if IsShortCode(dest) {
		for index := range parts {
			address := parts[index].DA
			address.SetTypeOfNumber(tpdu.TonUnknown)
			address.SetNumberingPlan(tpdu.NpISDN)
			parts[index].DA = address
		}
	}
	return parts, nil
}
