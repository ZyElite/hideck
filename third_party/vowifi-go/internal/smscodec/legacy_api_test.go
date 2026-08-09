package smscodec

import (
	"testing"
	"time"
)

var (
	_ func(string) (SMSEncoding, error)                            = NormalizeSMSEncoding
	_ func([]byte) ([]byte, error)                                 = DecodeBodyMaybeHex
	_ func([]byte) RPDUInfo                                        = ClassifyRPDU
	_ func([]byte) (byte, error)                                   = ParseRPErrorCause
	_ func([]byte) (byte, string, string, []byte, error)           = ParseRPDataWithAddresses
	_ func([]byte) (string, error)                                 = DecodeAddressValue
	_ func(string) []byte                                          = EncodeAddress
	_ func(byte, []byte, string) []byte                            = BuildRPData
	_ func([]byte) (string, string, time.Time, ConcatInfo, error)  = DecodeDeliverTPDU
	_ func(string) bool                                            = IsShortCode
	_ func(string, string, SubmitOptions) ([][]byte, []int, error) = BuildSubmitTPDUsWithOptions
	_ func([]byte) ([]byte, bool)                                  = TrimDeliverTPDUToDeclaredLength
	_ func([]byte) (int, bool)                                     = DeliverTPDUDeclaredLength
	_ func([]byte) (*OmaCPConfig, error)                           = DecodeOmaCPFromTPDU
	_ func(*OmaCPConfig) string                                    = FormatOmaCPSummary
)

func TestDecodeBodyMaybeHex(t *testing.T) {
	decoded, err := DecodeBodyMaybeHex([]byte(" 0001Aaff "))
	if err != nil || string(decoded) != string([]byte{0x00, 0x01, 0xaa, 0xff}) {
		t.Fatalf("DecodeBodyMaybeHex() = (%x, %v)", decoded, err)
	}
	raw := []byte("not hex")
	decoded, err = DecodeBodyMaybeHex(raw)
	if err != nil || string(decoded) != string(raw) {
		t.Fatalf("raw DecodeBodyMaybeHex() = (%q, %v)", decoded, err)
	}
	if _, err := DecodeBodyMaybeHex([]byte("  ")); err == nil {
		t.Fatal("empty body did not fail")
	}
}

func TestAddressTONAndBCDValidation(t *testing.T) {
	tests := []struct {
		number string
		toa    byte
	}{
		{number: "+447802002606", toa: 0x91},
		{number: "85075", toa: 0x81},
	}
	for _, test := range tests {
		encoded := EncodeAddress(test.number)
		if len(encoded) < 2 || encoded[1] != test.toa {
			t.Fatalf("EncodeAddress(%q) = %x", test.number, encoded)
		}
		decoded, err := DecodeAddressValue(encoded[1:])
		if err != nil || decoded != test.number {
			t.Fatalf("DecodeAddressValue(%q) = (%q, %v)", test.number, decoded, err)
		}
	}
	if _, err := DecodeAddressValue([]byte{0x81, 0x1a}); err == nil {
		t.Fatal("invalid BCD digit did not fail")
	}
}

func TestDecodeDeliverTPDUMessageKeepsZeroValuesOnError(t *testing.T) {
	decoded := DecodeDeliverTPDUMessage([]byte{0xff})
	if decoded.Err == nil {
		t.Fatal("invalid TPDU did not fail")
	}
	if decoded.TotalParts != 0 || decoded.PartNo != 0 || !decoded.Timestamp.IsZero() {
		t.Fatalf("failed decode leaked defaults: %+v", decoded)
	}
}
