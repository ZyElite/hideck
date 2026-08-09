package swu

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/iniwex5/vowifi-go/engine/ikev2"
)

func TestControlLoopAnswersPeerDPDWhileLocalRequestIsPending(t *testing.T) {
	session, transport := newEstablishedControlSession(t)
	defer stopControlTestSession(session)
	result := make(chan error, 1)
	go func() { result <- session.DPDProbe() }()

	localRaw := receiveSentIKE(t, transport)
	localRequest, err := ikev2.DecodePacket(localRaw)
	if err != nil {
		t.Fatalf("decode local DPD: %v", err)
	}
	peerRequest := &ikev2.IKEPacket{
		Header: newIKEHeader(
			session.spiI, session.spiR, ikev2.INFORMATIONAL, 0, 41,
		),
	}
	peerRaw, err := session.encryptAndWrap(peerRequest)
	if err != nil {
		t.Fatalf("encrypt peer DPD: %v", err)
	}
	transport.ike <- peerRaw

	peerResponseRaw := receiveSentIKE(t, transport)
	peerResponse, err := ikev2.DecodePacket(peerResponseRaw)
	if err != nil || peerResponse.MessageID != 41 ||
		peerResponse.Flags != ikeInitiatorFlag|ikeResponseFlag {
		t.Fatalf("peer DPD response id=%d flags=%02x err=%v", peerResponse.MessageID, peerResponse.Flags, err)
	}

	localResponse := &ikev2.IKEPacket{
		Header: newIKEHeader(
			session.spiI, session.spiR, ikev2.INFORMATIONAL,
			ikeResponseFlag, localRequest.MessageID,
		),
	}
	localResponseRaw, err := session.encryptAndWrap(localResponse)
	if err != nil {
		t.Fatalf("encrypt local DPD response: %v", err)
	}
	transport.ike <- localResponseRaw
	select {
	case err := <-result:
		if err != nil {
			t.Fatalf("local DPD: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("local DPD transaction did not complete")
	}
}

func TestControlLoopRejectsUnexpectedEstablishedSPIs(t *testing.T) {
	session, transport := newEstablishedControlSession(t)
	defer stopControlTestSession(session)
	invalid, err := (&ikev2.IKEPacket{
		Header: &ikev2.IKEHeader{
			SPIi: 99, SPIr: 100, Version: 0x20,
			ExchangeType: ikev2.INFORMATIONAL, Flags: ikeResponseFlag, MessageID: 1,
		},
	}).Encode()
	if err != nil {
		t.Fatalf("encode invalid response: %v", err)
	}
	transport.ike <- invalid
	select {
	case <-session.done:
	case <-time.After(time.Second):
		t.Fatal("invalid established response did not terminate control loop")
	}
	if terminal := session.TerminalError(); terminal == nil || !strings.Contains(terminal.Error(), "unexpected SPIs") {
		t.Fatalf("terminal error = %v", terminal)
	}
}

func TestControlLoopRetainsResponseUntilLegacyWaiterRegisters(t *testing.T) {
	session, transport := newEstablishedControlSession(t)
	defer stopControlTestSession(session)
	response, err := (&ikev2.IKEPacket{Header: newIKEHeader(
		session.spiI, session.spiR, ikev2.IKE_AUTH, ikeResponseFlag, 77,
	)}).Encode()
	if err != nil {
		t.Fatalf("encode early response: %v", err)
	}
	transport.ike <- response
	deadline := time.Now().Add(time.Second)
	key := ikeWaitKey{exchangeType: ikev2.IKE_AUTH, msgID: 77}
	for {
		session.controlMu.RLock()
		_, pending := session.ikePending[key]
		session.controlMu.RUnlock()
		if pending {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("early response was not retained")
		}
		time.Sleep(time.Millisecond)
	}
	got, err := session.receiveIKEResponseWithTimeout(ikev2.IKE_AUTH, 77, time.Second)
	if err != nil || string(got) != string(response) {
		t.Fatalf("retained response len=%d err=%v", len(got), err)
	}
}

func TestEstablishedExchangePropagatesTaskManagerSendFailure(t *testing.T) {
	session, transport := newEstablishedControlSessionWithConfig(t, &Config{Retransmit: &RetransmitConfig{
		MaxRetries: 0, InitialDelay: time.Millisecond, Backoff: 1, PollInterval: time.Millisecond,
	}})
	defer stopControlTestSession(session)
	sendErr := errors.New("send failed")
	transport.sendIKEErr = sendErr

	err := session.DPDProbe()
	if !errors.Is(err, ErrWindowTimeout) || !strings.Contains(err.Error(), sendErr.Error()) {
		t.Fatalf("DPD send failure = %v", err)
	}
}

func receiveSentIKE(t *testing.T, transport *testIKETransport) []byte {
	t.Helper()
	select {
	case raw := <-transport.sentIKE:
		return raw
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for sent IKE packet")
		return nil
	}
}
