package swu

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"testing"
	"time"

	"github.com/iniwex5/vowifi-go/engine/ikev2"
)

func TestIKEAuthFragmentSetRetransmitsAsAUnit(t *testing.T) {
	session := newFragmentTestSession(false)
	session.cfg.Retransmit = &RetransmitConfig{
		MaxRetries: 1, InitialDelay: 2 * time.Millisecond, Backoff: 1,
	}
	transport := newTestIKETransport()
	session.socket = transport

	if err := session.sendIKEAuthRequest([]ikev2.Payload{fragmentTestPayload(900)}); err != nil {
		t.Fatalf("sendIKEAuthRequest() error = %v", err)
	}
	initialCount := int(transport.sendCount.Load())
	if initialCount < 2 {
		t.Fatalf("initial fragment count = %d", initialCount)
	}
	if _, err := session.receiveIKE(context.Background()); !errors.Is(err, ErrTaskTimeout) {
		t.Fatalf("receiveIKE() error = %v, want ErrTaskTimeout", err)
	}
	if got := int(transport.sendCount.Load()); got != initialCount*2 {
		t.Fatalf("send count after retransmit = %d, want %d", got, initialCount*2)
	}
}

func TestExplicitIKEResponseUsesSKFWhenNegotiated(t *testing.T) {
	session := newFragmentTestSession(false)
	transport := newTestIKETransport()
	session.socket = transport
	const messageID = 17
	if err := session.sendEncryptedResponseWithMsgID(
		[]ikev2.Payload{fragmentTestPayload(900)}, ikev2.INFORMATIONAL, messageID,
	); err != nil {
		t.Fatalf("sendEncryptedResponseWithMsgID() error = %v", err)
	}
	count := int(transport.sendCount.Load())
	if count < 2 {
		t.Fatalf("response fragment count = %d", count)
	}
	for index := 0; index < count; index++ {
		part := <-transport.sentIKE
		header, _, number, total := decodeFragmentMetadata(t, part)
		if header.MessageID != messageID || header.Flags&ikeResponseFlag == 0 {
			t.Fatalf("response fragment header = %#v", header)
		}
		if number != uint16(index+1) || total != uint16(count) {
			t.Fatalf("response fragment metadata = %d/%d", number, total)
		}
	}
}

func TestEstablishedExchangeAcceptsOutOfOrderSKFResponse(t *testing.T) {
	session := newFragmentTestSession(false)
	transport := newTestIKETransport()
	session.socket = transport
	defer session.Shutdown()

	type exchangeResult struct {
		raw []byte
		err error
	}
	result := make(chan exchangeResult, 1)
	go func() {
		raw, err := session.sendEncryptedWithRetry(
			[]ikev2.Payload{fragmentTestPayload(700)}, ikev2.CREATE_CHILD_SA,
		)
		result <- exchangeResult{raw: raw, err: err}
	}()

	firstRequest := receiveFragmentPacket(t, transport.sentIKE)
	requestHeader, _, _, requestTotal := decodeFragmentMetadata(t, firstRequest)
	for number := uint16(1); number < requestTotal; number++ {
		receiveFragmentPacket(t, transport.sentIKE)
	}

	peer := newFragmentTestSession(false)
	peer.mu.Lock()
	peer.localIKEInitiator = false
	peer.mu.Unlock()
	want := fragmentTestPayload(800)
	responses, err := peer.fragmentResponse(
		[]ikev2.Payload{want}, requestHeader.ExchangeType, requestHeader.MessageID,
	)
	if err != nil {
		t.Fatalf("peer fragmentResponse() error = %v", err)
	}
	for index := len(responses) - 1; index >= 0; index-- {
		transport.ike <- responses[index]
	}

	select {
	case got := <-result:
		if got.err != nil {
			t.Fatalf("sendEncryptedWithRetry() error = %v", got.err)
		}
		assertProtectedFragmentResult(t, session, got.raw, want)
	case <-time.After(time.Second):
		t.Fatal("fragmented established exchange did not complete")
	}
}

func receiveFragmentPacket(t *testing.T, packets <-chan []byte) []byte {
	t.Helper()
	select {
	case packet := <-packets:
		return packet
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for an IKE packet")
		return nil
	}
}

func assertProtectedFragmentResult(
	t *testing.T,
	session *Session,
	raw []byte,
	want *ikev2.RawPayload,
) {
	t.Helper()
	packet, err := ikev2.DecodePacket(raw)
	if err != nil {
		t.Fatalf("DecodePacket() error = %v", err)
	}
	payloads, err := session.decryptAndParse(packet)
	if err != nil {
		t.Fatalf("decryptAndParse() error = %v", err)
	}
	got, ok := payloads[0].(*ikev2.RawPayload)
	if len(payloads) != 1 || !ok || !bytes.Equal(got.Data, want.Data) {
		t.Fatal("established SKF response changed the inner payload")
	}
	header := packetIKEHeader(packet)
	if binary.BigEndian.Uint64(session.spiR[:]) != header.SPIr {
		t.Fatal("normalized SKF response changed the responder SPI")
	}
}
