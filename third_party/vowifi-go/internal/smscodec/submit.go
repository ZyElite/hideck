package smscodec

import (
	"strings"

	"github.com/warthog618/sms"
	"github.com/warthog618/sms/encoding/tpdu"
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
	text = strings.TrimSpace(text)

	options := []sms.EncoderOption{sms.To(dest)}
	if enc == "ucs2" {
		options = append(options, sms.WithCharset(0)) // UCS-2 charset
	}

	parts, err := sms.Encode([]byte(text), options...)
	if err != nil {
		return nil, err
	}
	if len(parts) == 0 {
		return nil, sms.ErrClosed // placeholder: empty message
	}
	return parts, nil
}
