package swu

import (
	"bytes"
	"context"
	"errors"
	"net"
	"sync/atomic"
	"testing"
	"time"

	"github.com/iniwex5/vowifi-go/engine/eap"
	"github.com/iniwex5/vowifi-go/engine/ikev2"
	"github.com/iniwex5/vowifi-go/engine/ipsec"
	"github.com/iniwex5/vowifi-go/engine/swu/eapaka"
)

func TestReceiveIKERetransmitsThenTimesOut(t *testing.T) {
	transport := newTestIKETransport()
	session := NewSession(&Config{Retransmit: &RetransmitConfig{
		MaxRetries: 2, InitialDelay: 5 * time.Millisecond, Backoff: 1,
	}})
	session.socket = transport

	if err := session.sendIKE(testIKEPacket(7)); err != nil {
		t.Fatalf("sendIKE() error = %v", err)
	}
	_, err := session.receiveIKE(context.Background())
	if !errors.Is(err, ErrTaskTimeout) {
		t.Fatalf("receiveIKE() error = %v, want ErrTaskTimeout", err)
	}
	if got := transport.sendCount.Load(); got != 3 {
		t.Fatalf("send count = %d, want initial request plus two retries", got)
	}
}

func TestSendIKEPropagatesTransportFailure(t *testing.T) {
	transportErr := errors.New("transport send failed")
	transport := newTestIKETransport()
	transport.sendIKEErr = transportErr
	session := NewSession(&Config{})
	session.socket = transport
	if err := session.sendIKE(testIKEPacket(9)); !errors.Is(err, transportErr) {
		t.Fatalf("sendIKE error = %v, want %v", err, transportErr)
	}
}

func TestReceiveIKEIgnoresUnrelatedMessageID(t *testing.T) {
	transport := newTestIKETransport()
	session := NewSession(&Config{Retransmit: &RetransmitConfig{
		MaxRetries: 0, InitialDelay: time.Second, Backoff: 1,
	}})
	session.socket = transport

	if err := session.sendIKE(testIKEPacket(9)); err != nil {
		t.Fatalf("sendIKE() error = %v", err)
	}
	transport.ike <- testIKEResponse(8)
	transport.ike <- testIKEResponse(9)
	response, err := session.receiveIKE(context.Background())
	if err != nil {
		t.Fatalf("receiveIKE() error = %v", err)
	}
	if response.MessageID != 9 {
		t.Fatalf("response message ID = %d, want 9", response.MessageID)
	}
}

func TestNextMessageIDStartsAfterIKESAInit(t *testing.T) {
	session := NewSession(&Config{})
	if first, second := session.nextMessageID(), session.nextMessageID(); first != 1 || second != 2 {
		t.Fatalf("message IDs = %d, %d; want 1, 2", first, second)
	}
}

func TestApplyEAPSuccessAdvancesToFinalAuth(t *testing.T) {
	session := NewSession(&Config{})
	session.stage = stageEAP
	session.responderAuthenticated = true
	session.eapKeys = eapaka.Keys{MSK: bytes.Repeat([]byte{0x11}, eapaka.KeyLengthMSK)}
	payload := &ikev2.EncryptedPayloadEAP{EAPMessage: (&eap.EAPPacket{
		Code: eap.CodeSuccess, Identifier: 3,
	}).Encode()}

	decision, err := session.applyEAPHandlingResult([]ikev2.Payload{payload})
	if err != nil {
		t.Fatalf("applyEAPHandlingResult() error = %v", err)
	}
	if decision != "final" || session.stage != stageFinal {
		t.Fatalf("decision = %q, stage = %d; want final", decision, session.stage)
	}
}

func TestIKEProtectionRoundTripPreservesInnerPayloadTypes(t *testing.T) {
	session := NewSession(&Config{})
	session.ikeKeys = testIKEKeys()
	packet := &ikev2.IKEPacket{
		Version: 0x20, ExchangeType: ikev2.ExchangeIKEAuth,
		Flags: 0x08, MessageID: 1,
		Payloads: []ikev2.Payload{
			&ikev2.EncryptedPayloadEAP{EAPMessage: []byte{eap.CodeSuccess, 1, 0, 4}},
			&ikev2.EncryptedPayloadCP{CFGType: ikev2.CFG_REQUEST},
		},
	}
	raw, err := session.encryptAndWrap(packet)
	if err != nil {
		t.Fatalf("encryptAndWrap() error = %v", err)
	}
	protected, err := ikev2.DecodePacket(raw)
	if err != nil {
		t.Fatalf("DecodePacket() error = %v", err)
	}
	payloads, err := session.decryptAndParse(protected)
	if err != nil {
		t.Fatalf("decryptAndParse() error = %v", err)
	}
	if len(payloads) != 2 || payloads[0].Type() != ikev2.PayloadEAP || payloads[1].Type() != ikev2.PayloadCP {
		t.Fatalf("inner payloads = %#v", payloads)
	}
}

func TestIKEProtectionRejectsTamperedPacket(t *testing.T) {
	session := NewSession(&Config{})
	session.ikeKeys = testIKEKeys()
	packet := &ikev2.IKEPacket{
		Version: 0x20, ExchangeType: ikev2.ExchangeIKEAuth,
		Flags: 0x08, MessageID: 1,
		Payloads: []ikev2.Payload{&ikev2.EncryptedPayloadAuth{AuthData: []byte("auth")}},
	}
	raw, err := session.encryptAndWrap(packet)
	if err != nil {
		t.Fatalf("encryptAndWrap() error = %v", err)
	}
	raw[len(raw)-1] ^= 0xff
	protected, err := ikev2.DecodePacket(raw)
	if err != nil {
		t.Fatalf("DecodePacket() error = %v", err)
	}
	if _, err := session.decryptAndParse(protected); err == nil {
		t.Fatal("decryptAndParse() accepted a tampered packet")
	}
}

func testIKEKeys() *IKEKeys {
	return &IKEKeys{
		SK_ai: bytes.Repeat([]byte{0x11}, 20), SK_ar: bytes.Repeat([]byte{0x22}, 20),
		SK_ei: bytes.Repeat([]byte{0x33}, 16), SK_er: bytes.Repeat([]byte{0x44}, 16),
	}
}

func testIKEPacket(messageID uint32) []byte {
	packet, err := (&ikev2.IKEPacket{
		Version: 0x20, ExchangeType: ikev2.ExchangeIKEInit,
		Flags: ikeInitiatorFlag, MessageID: messageID,
	}).Encode()
	if err != nil {
		panic(err)
	}
	return packet
}

func testIKEResponse(messageID uint32) []byte {
	packet, err := (&ikev2.IKEPacket{
		Version: 0x20, ExchangeType: ikev2.ExchangeIKEInit,
		Flags: ikeResponseFlag, MessageID: messageID,
	}).Encode()
	if err != nil {
		panic(err)
	}
	return packet
}

type testIKETransport struct {
	ike        chan []byte
	esp        chan []byte
	netEvents  chan ipsec.NetEvent
	sentIKE    chan []byte
	sendCount  atomic.Int32
	sendIKEErr error
}

func newTestIKETransport() *testIKETransport {
	return &testIKETransport{
		ike: make(chan []byte, 4), esp: make(chan []byte, 1),
		netEvents: make(chan ipsec.NetEvent, 1), sentIKE: make(chan []byte, 16),
	}
}

func (t *testIKETransport) IKEPackets() <-chan []byte            { return t.ike }
func (t *testIKETransport) ESPPackets() <-chan []byte            { return t.esp }
func (t *testIKETransport) NetEventsChan() <-chan ipsec.NetEvent { return t.netEvents }
func (t *testIKETransport) Start() error                         { return nil }
func (t *testIKETransport) Stop()                                {}
func (t *testIKETransport) SendIKE(raw []byte) error {
	t.sendCount.Add(1)
	if t.sendIKEErr != nil {
		return t.sendIKEErr
	}
	select {
	case t.sentIKE <- append([]byte(nil), raw...):
	default:
	}
	return nil
}
func (t *testIKETransport) SendESP([]byte) error     { return nil }
func (t *testIKETransport) SendNATKeepalive() error  { return nil }
func (t *testIKETransport) SetRemotePort(int)        {}
func (t *testIKETransport) LocalIP() net.IP          { return net.IPv4zero }
func (t *testIKETransport) RemoteIP() net.IP         { return net.IPv4zero }
func (t *testIKETransport) LocalPort() uint16        { return 0 }
func (t *testIKETransport) RemotePort() int          { return 0 }
func (t *testIKETransport) LocalAddrString() string  { return "" }
func (t *testIKETransport) RemoteAddrString() string { return "" }
func (t *testIKETransport) RawFD() (int, error)      { return -1, nil }
func (t *testIKETransport) SetUDPEncap() error       { return nil }
