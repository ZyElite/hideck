package swu

import (
	"testing"

	"github.com/iniwex5/vowifi-go/engine/ikev2"
)

func TestValidIKEResponseHeader(t *testing.T) {
	request := &ikev2.IKEPacket{
		InitiatorSPI: [8]byte{1}, ResponderSPI: [8]byte{2},
		ExchangeType: ikev2.ExchangeIKEAuth, Flags: 0x08, MessageID: 3,
	}
	response := &ikev2.IKEPacket{
		InitiatorSPI: [8]byte{1}, ResponderSPI: [8]byte{2},
		ExchangeType: ikev2.ExchangeIKEAuth, Flags: 0x20, MessageID: 3,
	}
	if !validIKEResponseHeader(response, request) {
		t.Fatal("matching responder header was rejected")
	}

	invalid := *response
	invalid.ExchangeType = ikev2.ExchangeInformational
	if validIKEResponseHeader(&invalid, request) {
		t.Fatal("response with a different exchange type was accepted")
	}
	invalid = *response
	invalid.Flags = 0x28
	if validIKEResponseHeader(&invalid, request) {
		t.Fatal("response carrying the initiator flag was accepted")
	}
	invalid = *response
	invalid.ResponderSPI = [8]byte{9}
	if validIKEResponseHeader(&invalid, request) {
		t.Fatal("response with a different responder SPI was accepted")
	}
}
