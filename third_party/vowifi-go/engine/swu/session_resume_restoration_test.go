package swu

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
	"testing"
	"time"

	enginecrypto "github.com/iniwex5/vowifi-go/engine/crypto"
	"github.com/iniwex5/vowifi-go/engine/ikev2"
)

var (
	_ func(*Session) error         = (*Session).performSessionResumption
	_ func(*Session, []byte) error = (*Session).handleIkeSessionResumeResp
	_ func(*Session) error         = (*Session).sendIkeAuthChildless
)

func TestSessionResumeRequestAndRecoveredKeySchedule(t *testing.T) {
	oldSKd := bytes.Repeat([]byte{0x31}, 32)
	session := NewSession(&Config{ResumeTicket: []byte("opaque-ticket"), ResumeOldSKd: oldSKd})
	copy(session.spiI[:], []byte("init-spi"))
	session.Ni = bytes.Repeat([]byte{0x41}, 32)

	request, _, err := session.buildSessionResumeRequest()
	if err != nil {
		t.Fatalf("buildSessionResumeRequest: %v", err)
	}
	if len(request.Payloads) != 2 || request.Payloads[0].Type() != ikev2.NiNr ||
		request.Payloads[1].Type() != ikev2.PayloadNotify {
		t.Fatalf("resume request payloads = %v", request.Payloads)
	}
	notify := request.Payloads[1].(*ikev2.EncryptedPayloadNotify)
	if notify.ProtocolID != 0 || notify.NotifyType != ikev2.TICKET_OPAQUE ||
		!bytes.Equal(notify.NotifyData, []byte("opaque-ticket")) {
		t.Fatalf("resume ticket notify = %#v", notify)
	}

	responderNonce := bytes.Repeat([]byte{0x52}, 32)
	responderSPI := ikeSPIBytes(0x1122334455667788)
	response := sessionResumeResponse(t, session.spiI, responderSPI, responderNonce, nil)
	if err := session.handleIkeSessionResumeResp(response); err != nil {
		t.Fatalf("handleIkeSessionResumeResp: %v", err)
	}
	expected := expectedRecoveredResumeSKd(t, session, oldSKd, responderNonce, responderSPI)
	if !bytes.Equal(session.ikeKeys.SK_d, expected) {
		t.Fatalf("resumed SK_d = %x, want %x", session.ikeKeys.SK_d, expected)
	}
	if session.spiR != responderSPI || !bytes.Equal(session.nr, responderNonce) {
		t.Fatalf("resumed SPIr/Nr = %x/%x", session.spiR, session.nr)
	}
}

func TestSessionResumeNACKClearsCredentialAndHandshakeState(t *testing.T) {
	transport := newTestIKETransport()
	callback := make(chan bool, 1)
	session := NewSession(&Config{
		ResumeTicket: []byte("rejected-ticket"),
		ResumeOldSKd: bytes.Repeat([]byte{0x61}, 32),
		OnTicketUpdate: func(ticket, skd []byte) {
			callback <- ticket == nil && skd == nil
		},
		Retransmit: &RetransmitConfig{MaxRetries: 0, InitialDelay: time.Second, Backoff: 1},
	})
	session.socket = transport
	go respondToSessionResume(t, transport, func(request *ikev2.IKEPacket) []byte {
		return sessionResumeResponse(t, request.InitiatorSPI, ikeSPIBytes(0x9988776655443322),
			bytes.Repeat([]byte{0x72}, 32), &ikev2.EncryptedPayloadNotify{NotifyType: ikev2.TICKET_NACK})
	})

	resumed, err := session.trySessionResumption(context.Background())
	if err == nil || resumed || !strings.Contains(err.Error(), "TICKET_NACK") {
		t.Fatalf("trySessionResumption = %t, %v", resumed, err)
	}
	ticket, oldSKd, complete := session.sessionResumptionCredentials()
	if len(ticket) != 0 || len(oldSKd) != 0 || complete {
		t.Fatalf("credentials survived NACK: %x/%x/%t", ticket, oldSKd, complete)
	}
	if session.spiI != ([8]byte{}) || session.spiR != ([8]byte{}) ||
		session.ikeKeys != nil || session.nextOutboundID != 1 {
		t.Fatalf("resume failure state was not reset: SPI=%x/%x keys=%p id=%d",
			session.spiI, session.spiR, session.ikeKeys, session.nextOutboundID)
	}
	select {
	case cleared := <-callback:
		if !cleared {
			t.Fatal("ticket callback did not receive explicit invalidation")
		}
	case <-time.After(time.Second):
		t.Fatal("ticket invalidation callback was not invoked")
	}
	session.Shutdown()
}

func TestConnectUsesProductionSessionResumptionPath(t *testing.T) {
	transport := newTestIKETransport()
	oldSKd := bytes.Repeat([]byte{0x81}, 32)
	updatedTicket := []byte("replacement-ticket")
	type ticketUpdate struct{ ticket, skd []byte }
	updateCh := make(chan ticketUpdate, 1)
	config := &Config{
		DeviceID: "resume-test", EPDGAddr: "198.51.100.10", APN: "ims",
		IMSI: "001010123456789", ResumeTicket: []byte("initial-ticket"), ResumeOldSKd: oldSKd,
		TransportFactory: func(_, _ string) (Transport, error) { return transport, nil },
		OnTicketUpdate: func(ticket, skd []byte) {
			updateCh <- ticketUpdate{append([]byte(nil), ticket...), append([]byte(nil), skd...)}
			if len(ticket) > 0 {
				ticket[0] ^= 0xff
			}
			if len(skd) > 0 {
				skd[0] ^= 0xff
			}
		},
		Retransmit: &RetransmitConfig{MaxRetries: 0, InitialDelay: time.Second, Backoff: 1},
	}
	session := NewSession(config)
	peerErr := make(chan error, 1)
	go func() { peerErr <- runSessionResumePeer(transport, config, oldSKd, updatedTicket) }()

	ctx, cancel := context.WithCancel(context.Background())
	if err := session.Connect(ctx); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	defer session.Shutdown()
	if session.State() != stateEstablished || !session.sessionResumed {
		t.Fatalf("session state/resumed = %q/%t", session.State(), session.sessionResumed)
	}
	if session.espRemoteSPI != 0x55667788 || !session.innerIP.Equal(net.IPv4(10, 0, 0, 8)) {
		t.Fatalf("resumed CHILD_SA/config = %08x/%s", session.espRemoteSPI, session.innerIP)
	}
	select {
	case err := <-peerErr:
		if err != nil {
			t.Fatalf("resume peer: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("resume peer did not finish")
	}
	select {
	case update := <-updateCh:
		if !bytes.Equal(update.ticket, updatedTicket) || !bytes.Equal(update.skd, session.ikeKeys.SK_d) {
			t.Fatalf("ticket update = %x/%x", update.ticket, update.skd)
		}
	case <-time.After(time.Second):
		t.Fatal("replacement ticket was not persisted")
	}
	ticket, storedSKd, complete := session.sessionResumptionCredentials()
	if !complete || !bytes.Equal(ticket, updatedTicket) || !bytes.Equal(storedSKd, session.ikeKeys.SK_d) {
		t.Fatal("callback mutation changed session-owned resume credentials")
	}
	cancel()
	waitCtx, waitCancel := context.WithTimeout(context.Background(), time.Second)
	defer waitCancel()
	if err := session.WaitDoneContext(waitCtx); err != nil {
		t.Fatalf("parent context cancellation did not clean up session: %v", err)
	}
	if !transport.stopped.Load() || session.transport() != nil {
		t.Fatal("parent context cancellation retained the transport")
	}
}

func TestConnectFallsBackToIKESAInitAfterTicketNACK(t *testing.T) {
	transport := newTestIKETransport()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cleared := make(chan struct{}, 1)
	config := &Config{
		DeviceID: "resume-fallback", EPDGAddr: "198.51.100.20", APN: "ims",
		IMSI: "001010123456789", ResumeTicket: []byte("stale-ticket"),
		ResumeOldSKd:     bytes.Repeat([]byte{0xa1}, 32),
		TransportFactory: func(_, _ string) (Transport, error) { return transport, nil },
		OnTicketUpdate: func(ticket, skd []byte) {
			if ticket == nil && skd == nil {
				cleared <- struct{}{}
			}
		},
		Retransmit: &RetransmitConfig{MaxRetries: 0, InitialDelay: time.Second, Backoff: 1},
	}
	session := NewSession(config)
	sawInit := make(chan error, 1)
	go func() {
		resumeRaw, err := receiveResumeSentIKE(transport)
		if err != nil {
			sawInit <- err
			return
		}
		resume, err := ikev2.DecodePacket(resumeRaw)
		if err != nil {
			sawInit <- err
			return
		}
		transport.ike <- sessionResumeResponse(t, resume.InitiatorSPI, ikeSPIBytes(9),
			bytes.Repeat([]byte{0xb2}, 32), &ikev2.EncryptedPayloadNotify{NotifyType: ikev2.TICKET_NACK})
		initRaw, err := receiveResumeSentIKE(transport)
		if err == nil {
			var init *ikev2.IKEPacket
			init, err = ikev2.DecodePacket(initRaw)
			if err == nil && init.ExchangeType != ikev2.IKE_SA_INIT {
				err = fmt.Errorf("fallback exchange = %d, want IKE_SA_INIT", init.ExchangeType)
			}
		}
		sawInit <- err
		cancel()
	}()
	err := session.Connect(ctx)
	if err == nil || !errors.Is(err, context.Canceled) || !strings.Contains(err.Error(), "TICKET_NACK") {
		t.Fatalf("Connect fallback error = %v", err)
	}
	if fallbackErr := <-sawInit; fallbackErr != nil {
		t.Fatalf("fallback production path: %v", fallbackErr)
	}
	select {
	case <-cleared:
	default:
		t.Fatal("NACK did not invalidate persisted ticket")
	}
	if session.ikeControlIsRunning() || !transport.stopped.Load() {
		t.Fatal("failed fallback left control or transport running")
	}
	session.Shutdown()
}

func TestConnectCancellationPreservesTicketAndStopsControl(t *testing.T) {
	transport := newTestIKETransport()
	callbackCalled := make(chan struct{}, 1)
	config := &Config{
		DeviceID: "resume-cancel", EPDGAddr: "198.51.100.30",
		ResumeTicket: []byte("valid-ticket"), ResumeOldSKd: bytes.Repeat([]byte{0xc3}, 32),
		TransportFactory: func(_, _ string) (Transport, error) { return transport, nil },
		OnTicketUpdate:   func(_, _ []byte) { callbackCalled <- struct{}{} },
	}
	session := NewSession(config)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := session.Connect(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("Connect canceled error = %v", err)
	}
	ticket, oldSKd, complete := session.sessionResumptionCredentials()
	if !complete || !bytes.Equal(ticket, config.ResumeTicket) || !bytes.Equal(oldSKd, config.ResumeOldSKd) {
		t.Fatal("local cancellation invalidated reusable resume credentials")
	}
	select {
	case <-callbackCalled:
		t.Fatal("local cancellation invoked ticket invalidation callback")
	default:
	}
	if session.ikeControlIsRunning() || !transport.stopped.Load() {
		t.Fatal("canceled resume left control or transport running")
	}
	session.Shutdown()
}

func TestIKEControlGenerationCanBeStoppedConcurrently(t *testing.T) {
	session, _ := newEstablishedControlSession(t)
	errorsCh := make(chan error, 2)
	start := make(chan struct{})
	for range 2 {
		go func() {
			<-start
			errorsCh <- session.stopIKEControl()
		}()
	}
	close(start)
	for range 2 {
		if err := <-errorsCh; err != nil {
			t.Fatalf("stopIKEControl: %v", err)
		}
	}
	if session.ikeControlIsRunning() {
		t.Fatal("concurrent stop left IKE control running")
	}
	session.Shutdown()
}

func runSessionResumePeer(
	transport *testIKETransport,
	config *Config,
	oldSKd, updatedTicket []byte,
) error {
	resumeRaw, err := receiveResumeSentIKE(transport)
	if err != nil {
		return err
	}
	resume, err := ikev2.DecodePacket(resumeRaw)
	if err != nil {
		return fmt.Errorf("decode resume request: %w", err)
	}
	if resume.ExchangeType != ikev2.IKE_SESSION_RESUME || resume.MessageID != 0 {
		return fmt.Errorf("first request was exchange %d message %d", resume.ExchangeType, resume.MessageID)
	}
	initiatorNonce, err := sessionResumeNonce(resume.Payloads)
	if err != nil {
		return fmt.Errorf("parse resume request: %w", err)
	}
	responderSPI := ikeSPIBytes(0x1020304050607080)
	responderNonce := bytes.Repeat([]byte{0x91}, 32)
	response, err := sessionResumeResponseForPeer(resume.InitiatorSPI, responderSPI, responderNonce)
	if err != nil {
		return err
	}
	transport.ike <- response

	peer := NewSession(&Config{
		IKEEncryption: config.IKEEncryption, IKEEncryptionKeyBits: config.IKEEncryptionKeyBits,
		IKEPRF: config.IKEPRF, IKEIntegrity: config.IKEIntegrity, IKEDH: config.IKEDH,
		ESPEncryption: config.ESPEncryption, ESPEncryptionKeyBits: config.ESPEncryptionKeyBits,
		ESPIntegrity: config.ESPIntegrity, ResumeOldSKd: oldSKd,
	})
	peer.spiI, peer.spiR = resume.InitiatorSPI, responderSPI
	peer.Ni = initiatorNonce
	keys, err := peer.deriveSessionResumeKeys(responderSPI, responderNonce)
	if err != nil {
		return fmt.Errorf("derive peer resume keys: %w", err)
	}
	peer.ikeKeys, peer.nr = keys, responderNonce
	return answerResumedIKEAuth(transport, peer, updatedTicket)
}

func answerResumedIKEAuth(transport *testIKETransport, peer *Session, ticket []byte) error {
	raw, err := receiveResumeSentIKE(transport)
	if err != nil {
		return err
	}
	request, err := ikev2.DecodePacket(raw)
	if err != nil {
		return fmt.Errorf("decode resumed IKE_AUTH: %w", err)
	}
	if request.ExchangeType != ikev2.IKE_AUTH || request.MessageID != 1 {
		return fmt.Errorf("second request was exchange %d message %d", request.ExchangeType, request.MessageID)
	}
	payloads, err := peer.decryptAndParse(request)
	if err != nil {
		return fmt.Errorf("decrypt resumed IKE_AUTH: %w", err)
	}
	if !hasPayloadType(payloads, ikev2.PayloadSA) || hasPayloadType(payloads, ikev2.PayloadEAP) {
		return fmt.Errorf("unexpected resumed IKE_AUTH payloads: %s", ikePayloadTypes(payloads))
	}
	responsePayloads := resumedChildResponsePayloads(peer, ticket)
	response := &ikev2.IKEPacket{
		Header:   newIKEHeader(peer.spiI, peer.spiR, ikev2.IKE_AUTH, ikev2.FlagResponse, 1),
		Payloads: responsePayloads,
	}
	encoded, err := peer.encryptAndWrap(response)
	if err != nil {
		return fmt.Errorf("encrypt resumed IKE_AUTH response: %w", err)
	}
	transport.ike <- encoded
	return nil
}

func resumedChildResponsePayloads(peer *Session, ticket []byte) []ikev2.Payload {
	innerIP := net.IPv4(10, 0, 0, 8)
	return []ikev2.Payload{
		&ikev2.EncryptedPayloadCP{CFGType: ikev2.CFG_REPLY, Attributes: []*ikev2.CPAttribute{
			{Type: ikev2.CPAttrIP4Address, Value: innerIP.To4()},
			{Type: ikev2.CPAttrIP4DNS, Value: net.IPv4(1, 1, 1, 1).To4()},
		}},
		&ikev2.EncryptedPayloadSA{Proposals: buildESPProposalsForSession(peer, 0x55667788)},
		&ikev2.EncryptedPayloadTS{IsInitiator: true, TrafficSelectors: []*ikev2.TrafficSelector{
			ikev2.NewTrafficSelectorIPV4(innerIP, 0, 0, 0xffff),
		}},
		&ikev2.EncryptedPayloadTS{TrafficSelectors: []*ikev2.TrafficSelector{
			ikev2.NewTrafficSelectorIPV4(net.IPv4(192, 0, 2, 1), 0, 0, 0xffff),
		}},
		&ikev2.EncryptedPayloadNotify{NotifyType: ikev2.TICKET_OPAQUE, NotifyData: append([]byte(nil), ticket...)},
	}
}

func sessionResumeResponse(
	t *testing.T,
	initiatorSPI, responderSPI [8]byte,
	responderNonce []byte,
	notify *ikev2.EncryptedPayloadNotify,
) []byte {
	t.Helper()
	payloads := []ikev2.Payload{&ikev2.EncryptedPayloadNonce{NonceData: responderNonce}}
	if notify != nil {
		payloads = append(payloads, notify)
	}
	packet := &ikev2.IKEPacket{
		Header:   newIKEHeader(initiatorSPI, responderSPI, ikev2.IKE_SESSION_RESUME, ikev2.FlagResponse, 0),
		Payloads: payloads,
	}
	raw, err := packet.Encode()
	if err != nil {
		t.Fatalf("encode resume response: %v", err)
	}
	return raw
}

func sessionResumeResponseForPeer(initiatorSPI, responderSPI [8]byte, nonce []byte) ([]byte, error) {
	packet := &ikev2.IKEPacket{
		Header: newIKEHeader(initiatorSPI, responderSPI, ikev2.IKE_SESSION_RESUME, ikev2.FlagResponse, 0),
		Payloads: []ikev2.Payload{
			&ikev2.EncryptedPayloadNonce{NonceData: nonce},
			&ikev2.EncryptedPayloadNotify{NotifyType: ikev2.TICKET_ACK},
		},
	}
	raw, err := packet.Encode()
	if err != nil {
		return nil, fmt.Errorf("encode session resume response: %w", err)
	}
	return raw, nil
}

func expectedRecoveredResumeSKd(
	t *testing.T,
	session *Session,
	oldSKd, responderNonce []byte,
	responderSPI [8]byte,
) []byte {
	t.Helper()
	seedInput := append(append([]byte(nil), session.Ni...), responderNonce...)
	skeyseed := session.prf.Compute(oldSKd, seedInput)
	keySeed := append(append([]byte(nil), responderNonce...), session.Ni...)
	keySeed = append(keySeed, session.spiI[:]...)
	keySeed = append(keySeed, responderSPI[:]...)
	material, err := enginecrypto.PrfPlus(session.prf, skeyseed, keySeed,
		3*enginecrypto.PRFOutputSize(session.prf)+2*session.integKeyLen+2*session.encKeyLen)
	if err != nil {
		t.Fatalf("derive expected resume keys: %v", err)
	}
	return material[:enginecrypto.PRFOutputSize(session.prf)]
}

func respondToSessionResume(
	t *testing.T,
	transport *testIKETransport,
	response func(*ikev2.IKEPacket) []byte,
) {
	t.Helper()
	raw, err := receiveResumeSentIKE(transport)
	if err != nil {
		t.Errorf("receive resume request: %v", err)
		return
	}
	request, err := ikev2.DecodePacket(raw)
	if err != nil {
		t.Errorf("decode resume request: %v", err)
		return
	}
	transport.ike <- response(request)
}

func receiveResumeSentIKE(transport *testIKETransport) ([]byte, error) {
	select {
	case raw := <-transport.sentIKE:
		return raw, nil
	case <-time.After(time.Second):
		return nil, errors.New("timed out waiting for IKE request")
	}
}

func TestSessionResumeResponseRejectsInvalidNonceAndHeader(t *testing.T) {
	session := NewSession(&Config{ResumeTicket: []byte("ticket"), ResumeOldSKd: bytes.Repeat([]byte{1}, 32)})
	copy(session.spiI[:], []byte("init-spi"))
	session.Ni = bytes.Repeat([]byte{2}, 32)
	response := sessionResumeResponse(t, session.spiI, ikeSPIBytes(7), []byte{1, 2, 3}, nil)
	if err := session.handleIkeSessionResumeResp(response); err == nil {
		t.Fatal("short responder nonce was accepted")
	}
	response[18] = byte(ikev2.IKE_SA_INIT)
	if err := session.handleIkeSessionResumeResp(response); err == nil {
		t.Fatal("wrong exchange type was accepted")
	}
}
