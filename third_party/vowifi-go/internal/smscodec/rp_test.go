package smscodec

import (
	"bytes"
	"testing"
)

func TestBuildAndParseRPDataPreservesAddressOrder(t *testing.T) {
	tpdu := []byte{0x01, 0x02, 0x03}
	body := BuildRPData(0x22, "", "+447802002606", tpdu)
	if body[0] != 0x00 || body[2] != 0x00 {
		t.Fatalf("mobile-originated RP-DATA prefix = %x", body[:3])
	}
	mr, originator, destination, parsedTPDU, err := ParseRPDataWithAddresses(body)
	if err != nil {
		t.Fatal(err)
	}
	if mr != 0x22 || originator != "" || destination != "+447802002606" || !bytes.Equal(parsedTPDU, tpdu) {
		t.Fatalf("parsed RP-DATA = mr:%x oa:%q da:%q tpdu:%x", mr, originator, destination, parsedTPDU)
	}
}

func TestBuildRPControlPDUs(t *testing.T) {
	if got := BuildRPAck(0x33); !bytes.Equal(got, []byte{0x02, 0x33}) {
		t.Fatalf("RP-ACK = %x", got)
	}
	if got := BuildRPError(0x44, 41); !bytes.Equal(got, []byte{0x04, 0x44, 0x01, 41, 0x00}) {
		t.Fatalf("RP-ERROR = %x", got)
	}
}
