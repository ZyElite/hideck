package smscodec

import (
	"reflect"
	"testing"
)

// buildOmaCPWBXML constructs the WBXML bytes for:
//
//	<wap-provisioningdoc version="1.0">
//	  <characteristic type="BOOTSTRAP">
//	    <parm name="NAME" value="Test"/>
//	  </characteristic>
//	</wap-provisioningdoc>
func buildOmaCPWBXML(t *testing.T) []byte {
	t.Helper()
	b := []byte{0x02, 0x0B, 0x6A, 0x00} // version 1.2, public id 0x0B, UTF-8, no strtab
	// wap-provisioningdoc (tag 5) with attributes
	b = append(b, 0x80|5)
	b = append(b, attrStr(8, "1.0")...)
	b = append(b, 0x00) // end attrs
	// <characteristic type="BOOTSTRAP">
	b = append(b, 0x80|6)
	b = append(b, attrStr(7, "BOOTSTRAP")...)
	b = append(b, 0x00)
	// <parm name="NAME" value="Test"/>
	b = append(b, 0x80|7)
	b = append(b, attrStr(5, "NAME")...)
	b = append(b, attrStr(6, "Test")...)
	b = append(b, 0x00)
	b = append(b, 0x01) // end parm
	b = append(b, 0x01) // end characteristic
	b = append(b, 0x01) // end wap-provisioningdoc
	return b
}

// attrStr encodes a known attribute (token | 0x80) whose value is an inline
// string: <token> <len> <bytes> (WAP-192 "attribute with value").
func attrStr(tok byte, val string) []byte {
	b := []byte{0x80 | tok, byte(len(val))}
	return append(b, val...)
}

func TestDecodeOmaCPFromTPDU(t *testing.T) {
	data := buildOmaCPWBXML(t)
	chars, err := DecodeOmaCPFromTPDU(data)
	if err != nil {
		t.Fatalf("DecodeOmaCPFromTPDU: %v", err)
	}
	want := []OmaCPCharacteristic{
		{Type: "BOOTSTRAP"},
		{Name: "NAME", Value: "Test"},
	}
	if !reflect.DeepEqual(chars, want) {
		t.Errorf("got %+v, want %+v", chars, want)
	}

	summary := FormatOmaCPSummary(chars)
	wantSummary := "characteristic type=BOOTSTRAP\nparm name=NAME value=Test"
	if summary != wantSummary {
		t.Errorf("summary = %q, want %q", summary, wantSummary)
	}
}

func TestDecodeOmaCPFromTPDU_WithPrefix(t *testing.T) {
	// findWBXMLStart must locate the header even when preceded by a few bytes
	// (as in a WSP push body).
	data := append([]byte{0x81, 0x01, 0x00}, buildOmaCPWBXML(t)...)
	chars, err := DecodeOmaCPFromTPDU(data)
	if err != nil {
		t.Fatalf("DecodeOmaCPFromTPDU with prefix: %v", err)
	}
	if len(chars) != 2 {
		t.Fatalf("got %d chars, want 2", len(chars))
	}
}

func TestFormatOmaCPSummary_Empty(t *testing.T) {
	if got := FormatOmaCPSummary(nil); got != "OMA CP \n" {
		t.Errorf("empty summary = %q, want %q", got, "OMA CP \n")
	}
}

func TestParseWSPPush(t *testing.T) {
	// PUSH PDU: TID=0x01, PDU type=0x06, content-type token 0x2C (MMS),
	// then a header field with a length-prefixed name and value:
	//   content-location: http://mms.example.com/x.mms
	url := "http://mms.example.com/x.mms"
	payload := append([]byte{
		0x01, 0x06, // TID + PUSH
		0x2c, // content-type: application/vnd.wap.mms-message
		0x10, // header name length
	}, "content-location"...)
	payload = append(payload, byte(len(url)))
	payload = append(payload, url...)

	cls := parseWSPPush(payload)
	if cls.ContentType != "application/vnd.wap.mms-message" {
		t.Errorf("content type = %q", cls.ContentType)
	}
	if cls.URL != url {
		t.Errorf("url = %q, want %q", cls.URL, url)
	}
}
