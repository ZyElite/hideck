package swu

import (
	"bytes"
	"testing"
	"time"

	enginecrypto "github.com/iniwex5/vowifi-go/engine/crypto"
	"github.com/iniwex5/vowifi-go/engine/ikev2"
)

func TestEstablishedChildSARekeyValidatesResponseAndSwitchesSAs(t *testing.T) {
	session, transport := newEstablishedControlSession(t)
	defer stopControlTestSession(session)
	const remoteSPI = uint32(0xa1b2c3d4)
	responderNonce := bytes.Repeat([]byte{0x92}, 32)
	go respondToChildSARekey(t, session, transport, remoteSPI, responderNonce)

	oldLocalSPI := session.espLocalSPI
	if err := session.RekeyChildSA(); err != nil {
		t.Fatalf("RekeyChildSA: %v", err)
	}
	if session.espLocalSPI == oldLocalSPI || session.espRemoteSPI != remoteSPI {
		t.Fatalf("rekeyed SPIs local=%08x remote=%08x", session.espLocalSPI, session.espRemoteSPI)
	}
	if !bytes.Equal(session.childNr, responderNonce) {
		t.Fatalf("responder nonce = %x", session.childNr)
	}
}

func TestPeerChildSARekeyIsValidatedAndAnswered(t *testing.T) {
	session, transport := newEstablishedControlSession(t)
	defer stopControlTestSession(session)
	session.controlMu.Lock()
	session.controlRunning = false
	session.controlMu.Unlock()

	currentTSi, currentTSr := session.currentChildSelectors()
	const peerSPI = uint32(0xb1c2d3e4)
	request := &ikev2.IKEPacket{
		InitiatorSPI: session.SPIi, ResponderSPI: session.SPIr,
		Version: 0x20, ExchangeType: ikev2.ExchangeCreateChildSA,
		MessageID: 11,
		Payloads: []ikev2.Payload{
			&ikev2.EncryptedPayloadNotify{
				ProtocolID: ikev2.ProtoESP, SPISize: 4,
				NotifyType: ikev2.NotifyTypeRekeySA, SPI: spiBytes(session.espRemoteSPI),
			},
			&ikev2.EncryptedPayloadSA{Proposals: buildESPProposals(session.espCipher, session.espInteg, peerSPI)},
			&ikev2.EncryptedPayloadNonce{Data: bytes.Repeat([]byte{0x83}, 32)},
			retypeTrafficSelectorPayload(currentTSr, ikev2.PayloadTSi),
			retypeTrafficSelectorPayload(currentTSi, ikev2.PayloadTSr),
		},
	}
	raw, err := session.encryptAndWrap(request)
	if err != nil {
		t.Fatalf("encrypt peer rekey: %v", err)
	}
	decoded, err := ikev2.DecodePacket(raw)
	if err != nil {
		t.Fatalf("decode peer rekey: %v", err)
	}
	if err := session.handlePeerChildSARekey(decoded); err != nil {
		t.Fatalf("handlePeerChildSARekey: %v", err)
	}
	if session.espRemoteSPI != peerSPI {
		t.Fatalf("peer SPI = %08x", session.espRemoteSPI)
	}
	select {
	case response := <-transport.sentIKE:
		packet, err := ikev2.DecodePacket(response)
		if err != nil || packet.Flags&(ikeInitiatorFlag|ikeResponseFlag) != ikeInitiatorFlag|ikeResponseFlag {
			t.Fatalf("peer rekey response flags=%02x err=%v", packet.Flags, err)
		}
	default:
		t.Fatal("peer rekey response was not sent")
	}
}

func newEstablishedControlSession(t *testing.T) (*Session, *testIKETransport) {
	t.Helper()
	session := NewSession(&Config{Retransmit: &RetransmitConfig{
		MaxRetries: 0, InitialDelay: 200 * time.Millisecond, Backoff: 1,
	}})
	transport := newTestIKETransport()
	session.socket = transport
	copy(session.SPIi[:], []byte("init-spi"))
	copy(session.SPIr[:], []byte("resp-spi"))
	session.ikeKeys = testIKEKeys()
	session.ikeKeys.SK_d = bytes.Repeat([]byte{0x31}, enginecrypto.PRFOutputSize(session.prf))
	session.innerIP = []byte{10, 0, 0, 2}
	session.innerPrefix = 32
	session.espLocalSPI = 0x10203040
	session.espRemoteSPI = 0x50607080
	session.childNi = bytes.Repeat([]byte{0x41}, 32)
	session.childNr = bytes.Repeat([]byte{0x42}, 32)
	session.childTSi, session.childTSr = buildTrafficSelectorsForIPStack(session.innerIP)
	if err := session.setupDataPlane(); err != nil {
		t.Fatalf("setupDataPlane: %v", err)
	}
	session.setState(stateEstablished)
	if err := session.ensureIKEDispatcher(); err != nil {
		t.Fatalf("ensureIKEDispatcher: %v", err)
	}
	return session, transport
}

func respondToChildSARekey(
	t *testing.T,
	session *Session,
	transport *testIKETransport,
	remoteSPI uint32,
	responderNonce []byte,
) {
	t.Helper()
	select {
	case raw := <-transport.sentIKE:
		request, err := ikev2.DecodePacket(raw)
		if err != nil {
			t.Errorf("decode rekey request: %v", err)
			return
		}
		payloads, err := session.decryptAndParse(request)
		if err != nil {
			t.Errorf("decrypt rekey request: %v", err)
			return
		}
		_, _, tsi, tsr, err := collectChildSAPayloads(payloads)
		if err != nil {
			t.Errorf("collect rekey request: %v", err)
			return
		}
		response := &ikev2.IKEPacket{
			InitiatorSPI: request.InitiatorSPI, ResponderSPI: request.ResponderSPI,
			Version: 0x20, ExchangeType: request.ExchangeType,
			Flags: ikeResponseFlag, MessageID: request.MessageID,
			Payloads: []ikev2.Payload{
				&ikev2.EncryptedPayloadSA{Proposals: buildESPProposals(session.espCipher, session.espInteg, remoteSPI)},
				&ikev2.EncryptedPayloadNonce{Data: append([]byte(nil), responderNonce...)},
				cloneTrafficSelectorPayload(tsi),
				cloneTrafficSelectorPayload(tsr),
			},
		}
		encoded, err := session.encryptAndWrap(response)
		if err != nil {
			t.Errorf("encrypt rekey response: %v", err)
			return
		}
		transport.ike <- encoded
	case <-time.After(time.Second):
		t.Error("timed out waiting for rekey request")
	}
}

func stopControlTestSession(session *Session) {
	session.cancel()
	session.controlWG.Wait()
	session.stopDataPlane()
}

func TestDPDWaitsForMatchingEmptyResponse(t *testing.T) {
	session, transport := newEstablishedControlSession(t)
	defer stopControlTestSession(session)
	go func() {
		raw := <-transport.sentIKE
		request, _ := ikev2.DecodePacket(raw)
		response := &ikev2.IKEPacket{
			InitiatorSPI: request.InitiatorSPI, ResponderSPI: request.ResponderSPI,
			Version: 0x20, ExchangeType: request.ExchangeType,
			Flags: ikeResponseFlag, MessageID: request.MessageID,
		}
		encoded, _ := session.encryptAndWrap(response)
		transport.ike <- encoded
	}()
	if err := session.DPDProbe(); err != nil {
		t.Fatalf("DPDProbe: %v", err)
	}
}

func TestEstablishedIKESARekeySwitchesKeysAndResetsMessageID(t *testing.T) {
	session, transport := newEstablishedControlSession(t)
	defer stopControlTestSession(session)
	oldSPI := session.SPIi
	oldSKd := append([]byte(nil), session.ikeKeys.SK_d...)
	go respondToIKESARekey(t, session, transport)
	if err := session.RekeyIKESA(); err != nil {
		t.Fatalf("RekeyIKESA: %v", err)
	}
	if session.SPIi == oldSPI || bytes.Equal(session.ikeKeys.SK_d, oldSKd) {
		t.Fatal("IKE SA rekey did not replace SPI and keys")
	}
	if session.nextOutboundID != 0 || !session.localIKEInitiator {
		t.Fatalf("new IKE SA message ID=%d local initiator=%t", session.nextOutboundID, session.localIKEInitiator)
	}
}

func TestPeerIKESARekeyChangesOriginalInitiatorRole(t *testing.T) {
	session, transport := newEstablishedControlSession(t)
	defer stopControlTestSession(session)
	session.controlMu.Lock()
	session.controlRunning = false
	session.controlMu.Unlock()
	peerDH, err := enginecrypto.NewDiffieHellman(session.dhGroup)
	if err != nil || peerDH.GenerateKey() != nil {
		t.Fatalf("peer DH: %v", err)
	}
	var peerSPI [8]byte
	copy(peerSPI[:], []byte("peer-new"))
	proposals := buildIKEProposals(session.encrAlg, session.prfAlg, session.integAlg, session.dhGroup)
	proposals[0].SPI, proposals[0].SPISize = append([]byte(nil), peerSPI[:]...), 8
	request := &ikev2.IKEPacket{
		InitiatorSPI: session.SPIi, ResponderSPI: session.SPIr,
		Version: 0x20, ExchangeType: ikev2.ExchangeCreateChildSA, MessageID: 15,
		Payloads: []ikev2.Payload{
			&ikev2.EncryptedPayloadSA{Proposals: proposals},
			&ikev2.EncryptedPayloadNonce{Data: bytes.Repeat([]byte{0x73}, 32)},
			&ikev2.EncryptedPayloadKE{DHGroupNum: session.dhGroup, KeyData: peerDH.PublicKeyBytes()},
		},
	}
	raw, err := session.encryptAndWrap(request)
	if err != nil {
		t.Fatalf("encrypt peer IKE rekey: %v", err)
	}
	decoded, _ := ikev2.DecodePacket(raw)
	if err := session.handleIncomingCreateChildSAParsed(decoded); err != nil {
		t.Fatalf("handle peer IKE rekey: %v", err)
	}
	if session.localIKEInitiator || session.SPIi != peerSPI || session.nextOutboundID != 0 {
		t.Fatalf("peer-rekeyed role=%t SPIi=%x messageID=%d", session.localIKEInitiator, session.SPIi, session.nextOutboundID)
	}
	select {
	case response := <-transport.sentIKE:
		packet, err := ikev2.DecodePacket(response)
		if err != nil || packet.Flags != ikeInitiatorFlag|ikeResponseFlag {
			t.Fatalf("peer IKE rekey response flags=%02x err=%v", packet.Flags, err)
		}
	default:
		t.Fatal("peer IKE rekey response was not sent")
	}
}

func respondToIKESARekey(t *testing.T, session *Session, transport *testIKETransport) {
	t.Helper()
	raw := <-transport.sentIKE
	request, err := ikev2.DecodePacket(raw)
	if err != nil {
		t.Errorf("decode IKE rekey request: %v", err)
		return
	}
	payloads, err := session.decryptAndParse(request)
	if err != nil {
		t.Errorf("decrypt IKE rekey request: %v", err)
		return
	}
	var nonce []byte
	var peerKey []byte
	for _, payload := range payloads {
		switch payload.Type() {
		case ikev2.PayloadNi:
			nonce = childSANonceData(payload)
		case ikev2.PayloadKE:
			_, peerKey, err = parseKERaw(payload.(*ikev2.RawPayload))
		}
	}
	if err != nil || len(nonce) == 0 || len(peerKey) == 0 {
		t.Errorf("parse IKE rekey request nonce=%d key=%d err=%v", len(nonce), len(peerKey), err)
		return
	}
	responderDH, _ := enginecrypto.NewDiffieHellman(session.dhGroup)
	_ = responderDH.GenerateKey()
	var responderSPI [8]byte
	copy(responderSPI[:], []byte("new-resp"))
	proposals := buildIKEProposals(session.encrAlg, session.prfAlg, session.integAlg, session.dhGroup)
	proposals[0].SPI, proposals[0].SPISize = append([]byte(nil), responderSPI[:]...), 8
	response := &ikev2.IKEPacket{
		InitiatorSPI: request.InitiatorSPI, ResponderSPI: request.ResponderSPI,
		Version: 0x20, ExchangeType: request.ExchangeType,
		Flags: ikeResponseFlag, MessageID: request.MessageID,
		Payloads: []ikev2.Payload{
			&ikev2.EncryptedPayloadSA{Proposals: proposals},
			&ikev2.EncryptedPayloadNonce{Data: bytes.Repeat([]byte{0x74}, 32)},
			&ikev2.EncryptedPayloadKE{DHGroupNum: session.dhGroup, KeyData: responderDH.PublicKeyBytes()},
		},
	}
	encoded, _ := session.encryptAndWrap(response)
	transport.ike <- encoded

	deleteRaw := <-transport.sentIKE
	deleteRequest, _ := ikev2.DecodePacket(deleteRaw)
	deleteResponse := &ikev2.IKEPacket{
		InitiatorSPI: deleteRequest.InitiatorSPI, ResponderSPI: deleteRequest.ResponderSPI,
		Version: 0x20, ExchangeType: deleteRequest.ExchangeType,
		Flags: ikeResponseFlag, MessageID: deleteRequest.MessageID,
	}
	encoded, _ = session.encryptAndWrap(deleteResponse)
	transport.ike <- encoded
}
