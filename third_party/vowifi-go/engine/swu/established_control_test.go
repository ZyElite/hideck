package swu

import (
	"bytes"
	"encoding/binary"
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
	oldInbound := session.espInboundSA
	if err := session.RekeyChildSA(); err != nil {
		t.Fatalf("RekeyChildSA: %v", err)
	}
	if session.espLocalSPI == oldLocalSPI || session.espRemoteSPI != remoteSPI {
		t.Fatalf("rekeyed SPIs local=%08x remote=%08x", session.espLocalSPI, session.espRemoteSPI)
	}
	if !bytes.Equal(session.childNr, responderNonce) {
		t.Fatalf("responder nonce = %x", session.childNr)
	}
	if !allZero(oldInbound.EncryptionKey) || !allZero(oldInbound.IntegrityKey) {
		t.Fatal("retired inbound CHILD_SA keys were not wiped")
	}
}

func TestChildSARekeyKeepsOldInboundSAWhenDeleteTimesOut(t *testing.T) {
	session, transport := newEstablishedControlSessionWithConfig(t, &Config{Retransmit: &RetransmitConfig{
		MaxRetries: 0, InitialDelay: 50 * time.Millisecond, Backoff: 1, PollInterval: time.Millisecond,
	}})
	defer stopControlTestSession(session)
	oldLocalSPI, oldRemoteSPI := session.espLocalSPI, session.espRemoteSPI
	oldInbound := session.espInboundSA
	peerDone := make(chan struct{})
	go func() {
		defer close(peerDone)
		respondToChildSARekeyResponse(t, session, transport, 0xa1b2c3d4, bytes.Repeat([]byte{0x92}, 32))
		receiveFragmentPacket(t, transport.sentIKE)
	}()
	err := session.RekeyChildSA()
	<-peerDone
	if err != nil {
		t.Fatalf("RekeyChildSA: %v", err)
	}
	assertRetiredChildSA(t, session, oldRemoteSPI, oldLocalSPI, true)
	if allZero(oldInbound.EncryptionKey) {
		t.Fatal("overlap CHILD_SA was wiped before peer Delete")
	}
}

func TestEstablishedChildSARekeyPreservesPFS(t *testing.T) {
	session, transport := newEstablishedControlSession(t)
	defer stopControlTestSession(session)
	oldDH, err := enginecrypto.NewDiffieHellman(14)
	if err != nil || oldDH.GenerateKey() != nil {
		t.Fatalf("initial child DH: %v", err)
	}
	session.childDH = oldDH
	go respondToChildSARekey(t, session, transport, 0xa1b2c3d4, bytes.Repeat([]byte{0x92}, 32))
	if err := session.RekeyChildSA(); err != nil {
		t.Fatalf("RekeyChildSA: %v", err)
	}
	if session.childDH == nil || session.childDH == oldDH || len(session.childDHSecret) == 0 {
		t.Fatal("PFS rekey did not install fresh DH state and shared secret")
	}
}

func TestPeerChildSARekeyIsValidatedAndAnswered(t *testing.T) {
	session, transport := newEstablishedControlSession(t)
	defer stopControlTestSession(session)
	session.controlMu.Lock()
	session.controlRunning = false
	session.controlMu.Unlock()
	oldLocalSPI, oldRemoteSPI := session.espLocalSPI, session.espRemoteSPI
	oldInbound := session.espInboundSA

	currentTSi, currentTSr := session.currentChildSelectors()
	const peerSPI = uint32(0xb1c2d3e4)
	request := &ikev2.IKEPacket{
		InitiatorSPI: session.spiI, ResponderSPI: session.spiR,
		Version: 0x20, ExchangeType: ikev2.ExchangeCreateChildSA,
		MessageID: 11,
		Payloads: []ikev2.Payload{
			&ikev2.EncryptedPayloadNotify{
				ProtocolID: ikev2.ProtoESP, SPISize: 4,
				NotifyType: ikev2.NotifyTypeRekeySA, SPI: spiBytes(session.espRemoteSPI),
			},
			&ikev2.EncryptedPayloadSA{Proposals: buildESPProposalsForSession(session, peerSPI)},
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
	assertRetiredChildSA(t, session, oldRemoteSPI, oldLocalSPI, true)
	request = &ikev2.IKEPacket{
		InitiatorSPI: session.spiI, ResponderSPI: session.spiR,
		Version: 0x20, ExchangeType: ikev2.ExchangeInformational, MessageID: 12,
		Payloads: []ikev2.Payload{&ikev2.EncryptedPayloadDelete{
			ProtocolID: ikev2.ProtoESP, SPISize: 4, NumSPIs: 1, SPIs: spiBytes(oldLocalSPI),
		}},
	}
	raw, err = session.encryptAndWrap(request)
	if err != nil {
		t.Fatalf("encrypt peer Delete: %v", err)
	}
	decoded, err = ikev2.DecodePacket(raw)
	if err != nil {
		t.Fatalf("decode peer Delete: %v", err)
	}
	if err := session.handlePeerInformational(decoded); err != nil {
		t.Fatalf("handle peer Delete: %v", err)
	}
	assertRetiredChildSA(t, session, oldRemoteSPI, oldLocalSPI, false)
	if !allZero(oldInbound.EncryptionKey) || !allZero(oldInbound.IntegrityKey) {
		t.Fatal("peer-deleted CHILD_SA keys were not wiped")
	}
	assertChildSADeleteResponse(t, session, transport, oldRemoteSPI)
}

func assertChildSADeleteResponse(
	t *testing.T,
	session *Session,
	transport *testIKETransport,
	wantSPI uint32,
) {
	t.Helper()
	raw := receiveFragmentPacket(t, transport.sentIKE)
	packet, err := ikev2.DecodePacket(raw)
	if err != nil {
		t.Fatalf("decode peer Delete response: %v", err)
	}
	payloads, err := session.decryptAndParse(packet)
	if err != nil || len(payloads) != 1 {
		t.Fatalf("peer Delete response payloads=%d err=%v", len(payloads), err)
	}
	deletion, ok := payloads[0].(*ikev2.EncryptedPayloadDelete)
	if !ok || len(deletion.SPIs) != 4 || binary.BigEndian.Uint32(deletion.SPIs) != wantSPI {
		t.Fatalf("peer Delete response = %#v, want SPI %08x", payloads[0], wantSPI)
	}
}

func assertRetiredChildSA(t *testing.T, session *Session, remoteSPI, localSPI uint32, present bool) {
	t.Helper()
	session.childSAMu.RLock()
	mapped, mappedOK := session.retiredChildSAs[localSPI]
	_, inboundOK := session.espInboundSAs[localSPI]
	session.childSAMu.RUnlock()
	if mappedOK != present || inboundOK != present || present && mapped != remoteSPI {
		t.Fatalf("retired mapping=%08x/%t inbound=%t, want present=%t", mapped, mappedOK, inboundOK, present)
	}
}

func newEstablishedControlSession(t *testing.T) (*Session, *testIKETransport) {
	t.Helper()
	return newEstablishedControlSessionWithConfig(t, &Config{Retransmit: &RetransmitConfig{
		MaxRetries: 0, InitialDelay: 200 * time.Millisecond, Backoff: 1,
	}})
}

func newEstablishedControlSessionWithConfig(t *testing.T, config *Config) (*Session, *testIKETransport) {
	t.Helper()
	session := NewSession(config)
	transport := newTestIKETransport()
	session.socket = transport
	copy(session.spiI[:], []byte("init-spi"))
	copy(session.spiR[:], []byte("resp-spi"))
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
	if err := session.startIKEControl(); err != nil {
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
	session.childSAMu.RLock()
	oldLocalSPI := session.espLocalSPI
	session.childSAMu.RUnlock()
	oldRemoteSPI := respondToChildSARekeyResponse(t, session, transport, remoteSPI, responderNonce)
	respondToChildSADelete(t, session, transport, oldRemoteSPI, oldLocalSPI)
}

func respondToChildSARekeyResponse(
	t *testing.T,
	session *Session,
	transport *testIKETransport,
	remoteSPI uint32,
	responderNonce []byte,
) uint32 {
	t.Helper()
	session.childSAMu.RLock()
	oldRemoteSPI := session.espRemoteSPI
	session.childSAMu.RUnlock()
	select {
	case raw := <-transport.sentIKE:
		request, err := ikev2.DecodePacket(raw)
		if err != nil {
			t.Errorf("decode rekey request: %v", err)
			return oldRemoteSPI
		}
		payloads, err := session.decryptAndParse(request)
		if err != nil {
			t.Errorf("decrypt rekey request: %v", err)
			return oldRemoteSPI
		}
		_, _, tsi, tsr, err := collectChildSAPayloads(payloads)
		if err != nil {
			t.Errorf("collect rekey request: %v", err)
			return oldRemoteSPI
		}
		responsePayloads := []ikev2.Payload{
			&ikev2.EncryptedPayloadSA{Proposals: buildESPProposalsForSession(session, remoteSPI)},
			&ikev2.EncryptedPayloadNonce{Data: append([]byte(nil), responderNonce...)},
		}
		responsePayloads, err = addChildSARekeyPFSResponse(payloads, responsePayloads)
		if err != nil {
			t.Errorf("build PFS rekey response: %v", err)
			return oldRemoteSPI
		}
		responsePayloads = append(responsePayloads, cloneTrafficSelectorPayload(tsi), cloneTrafficSelectorPayload(tsr))
		response := &ikev2.IKEPacket{
			InitiatorSPI: request.InitiatorSPI, ResponderSPI: request.ResponderSPI,
			Version: 0x20, ExchangeType: request.ExchangeType,
			Flags: ikeResponseFlag, MessageID: request.MessageID,
			Payloads: responsePayloads,
		}
		encoded, err := session.encryptAndWrap(response)
		if err != nil {
			t.Errorf("encrypt rekey response: %v", err)
			return oldRemoteSPI
		}
		transport.ike <- encoded
	case <-time.After(time.Second):
		t.Error("timed out waiting for rekey request")
	}
	return oldRemoteSPI
}

func addChildSARekeyPFSResponse(
	request []ikev2.Payload,
	response []ikev2.Payload,
) ([]ikev2.Payload, error) {
	for _, payload := range request {
		if payload == nil || payload.Type() != ikev2.PayloadKE {
			continue
		}
		group, _, err := parseKEPayload(payload)
		if err != nil {
			return nil, err
		}
		dh, err := enginecrypto.NewDiffieHellman(group)
		if err != nil {
			return nil, err
		}
		if err := dh.GenerateKey(); err != nil {
			return nil, err
		}
		sa := response[0].(*ikev2.EncryptedPayloadSA)
		sa.Proposals[0].AddTransform(ikev2.TransformTypeDH, ikev2.AlgorithmType(group), 0)
		return append(response, &ikev2.EncryptedPayloadKE{
			DHGroup: ikev2.AlgorithmType(group), KEData: dh.PublicKeyBytes(),
		}), nil
	}
	return response, nil
}

func respondToChildSADelete(
	t *testing.T,
	session *Session,
	transport *testIKETransport,
	wantRequestSPI uint32,
	responseSPI uint32,
) {
	t.Helper()
	deleteRaw := receiveFragmentPacket(t, transport.sentIKE)
	request, err := ikev2.DecodePacket(deleteRaw)
	if err != nil {
		t.Errorf("decode CHILD_SA delete request: %v", err)
		return
	}
	payloads, err := session.decryptAndParse(request)
	if err != nil || len(payloads) != 1 {
		t.Errorf("decrypt CHILD_SA delete payloads=%d err=%v", len(payloads), err)
		return
	}
	deletion, ok := payloads[0].(*ikev2.EncryptedPayloadDelete)
	if !ok || len(deletion.SPIs) != 4 || binary.BigEndian.Uint32(deletion.SPIs) != wantRequestSPI {
		t.Errorf("CHILD_SA delete = %#v, want SPI %08x", payloads[0], wantRequestSPI)
		return
	}
	response := &ikev2.IKEPacket{
		InitiatorSPI: request.InitiatorSPI, ResponderSPI: request.ResponderSPI,
		Version: 0x20, ExchangeType: request.ExchangeType,
		Flags: ikeResponseFlag, MessageID: request.MessageID,
		Payloads: childSADeleteResponse([]uint32{responseSPI}),
	}
	encoded, err := session.encryptAndWrap(response)
	if err != nil {
		t.Errorf("encrypt CHILD_SA delete response: %v", err)
		return
	}
	transport.ike <- encoded
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
	oldSPI := session.spiI
	oldKeys := session.ikeKeys
	oldSKd := append([]byte(nil), session.ikeKeys.SK_d...)
	go respondToIKESARekey(t, session, transport)
	if err := session.RekeyIKESA(); err != nil {
		t.Fatalf("RekeyIKESA: %v", err)
	}
	if session.spiI == oldSPI || bytes.Equal(session.ikeKeys.SK_d, oldSKd) {
		t.Fatal("IKE SA rekey did not replace SPI and keys")
	}
	if session.nextOutboundID != 0 || !session.localIKEInitiator {
		t.Fatalf("new IKE SA message ID=%d local initiator=%t", session.nextOutboundID, session.localIKEInitiator)
	}
	for name, key := range map[string][]byte{
		"SKEYSEED": oldKeys.SKEYSEED, "SK_d": oldKeys.SK_d, "SK_ai": oldKeys.SK_ai,
		"SK_ar": oldKeys.SK_ar, "SK_ei": oldKeys.SK_ei, "SK_er": oldKeys.SK_er,
		"SK_pi": oldKeys.SK_pi, "SK_pr": oldKeys.SK_pr,
	} {
		if !allZero(key) {
			t.Fatalf("old %s was not wiped", name)
		}
	}
}

func TestInitiatedIKESARekeyRetainsOldKeysWhenDeleteTimesOut(t *testing.T) {
	session, transport := newEstablishedControlSessionWithConfig(t, &Config{Retransmit: &RetransmitConfig{
		MaxRetries: 0, InitialDelay: 50 * time.Millisecond, Backoff: 1, PollInterval: time.Millisecond,
	}})
	defer stopControlTestSession(session)
	oldSPIi, oldSPIr := session.spiI, session.spiR
	oldKeys := session.ikeKeys
	peerDone := make(chan struct{})
	go func() {
		defer close(peerDone)
		respondToIKESARekeyResponse(t, session, transport)
		receiveFragmentPacket(t, transport.sentIKE)
	}()
	err := session.RekeyIKESA()
	<-peerDone
	if err != nil {
		t.Fatalf("RekeyIKESA: %v", err)
	}
	session.mu.RLock()
	retired := session.retiredIKESA
	retained := retired != nil && retired.spiI == oldSPIi && retired.spiR == oldSPIr &&
		retired.keys == oldKeys && !allZero(oldKeys.SK_d)
	session.mu.RUnlock()
	if !retained {
		t.Fatal("timed-out old IKE SA Delete did not retain the old protection context")
	}
}

func allZero(value []byte) bool {
	for _, item := range value {
		if item != 0 {
			return false
		}
	}
	return true
}

func TestPeerIKESARekeyChangesOriginalInitiatorRole(t *testing.T) {
	session, transport := newEstablishedControlSession(t)
	defer stopControlTestSession(session)
	session.controlMu.Lock()
	session.controlRunning = false
	session.controlMu.Unlock()
	oldSPIi, oldSPIr := session.spiI, session.spiR
	oldKeys := session.ikeKeys
	peerDH, err := enginecrypto.NewDiffieHellman(session.dhGroup)
	if err != nil || peerDH.GenerateKey() != nil {
		t.Fatalf("peer DH: %v", err)
	}
	var peerSPI [8]byte
	copy(peerSPI[:], []byte("peer-new"))
	proposals := buildIKEProposalsForSession(session)
	proposals[0].SPI, proposals[0].SPISize = append([]byte(nil), peerSPI[:]...), 8
	request := &ikev2.IKEPacket{
		InitiatorSPI: session.spiI, ResponderSPI: session.spiR,
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
	if err := session.handleIncomingCreateChildSAPacket(decoded); err != nil {
		t.Fatalf("handle peer IKE rekey: %v", err)
	}
	if session.localIKEInitiator || session.spiI != peerSPI || session.nextOutboundID != 0 {
		t.Fatalf("peer-rekeyed role=%t SPIi=%x messageID=%d", session.localIKEInitiator, session.spiI, session.nextOutboundID)
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
	session.mu.RLock()
	retained := session.retiredIKESA != nil && session.retiredIKESA.keys == oldKeys && !allZero(oldKeys.SK_d)
	session.mu.RUnlock()
	if !retained {
		t.Fatal("old IKE SA was not retained until peer Delete")
	}
	deleteKeys := cloneIKEKeysForTest(oldKeys)
	deleteRequest := &ikev2.IKEPacket{
		Header: newIKEHeader(oldSPIi, oldSPIr, ikev2.INFORMATIONAL, 0, 16),
		Payloads: []ikev2.Payload{&ikev2.EncryptedPayloadDelete{
			ProtocolID: ikev2.ProtoIKE,
		}},
	}
	raw, err = session.encryptAndWrapWithKeys(deleteRequest, 16, oldKeys)
	if err != nil {
		t.Fatalf("encrypt old IKE Delete: %v", err)
	}
	deleteHeader, err := ikev2.DecodeHeader(raw)
	if err != nil {
		t.Fatalf("decode old IKE Delete header: %v", err)
	}
	if _, retired, err := session.ikeContextForHeader(deleteHeader); err != nil || !retired {
		t.Fatalf("old IKE Delete context retired=%t err=%v header=%x/%x", retired, err, deleteHeader.SPIi, deleteHeader.SPIr)
	}
	transport.ike <- raw
	var deleteResponse []byte
	select {
	case deleteResponse = <-transport.sentIKE:
	case <-time.After(time.Second):
		t.Fatalf("old IKE Delete response timeout: %v", session.TerminalError())
	}
	responsePacket, err := ikev2.DecodePacket(deleteResponse)
	if err != nil {
		t.Fatalf("decode old IKE Delete response: %v", err)
	}
	responsePayloads, err := session.decryptAndParseWithKeys(responsePacket, deleteKeys)
	if err != nil || len(responsePayloads) != 0 {
		t.Fatalf("old IKE Delete response payloads=%d err=%v", len(responsePayloads), err)
	}
	deadline := time.Now().Add(time.Second)
	for !retiredIKEKeysCleared(session, oldKeys) && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if !retiredIKEKeysCleared(session, oldKeys) {
		t.Fatal("old IKE SA keys were not wiped after Delete")
	}
	transport.ike <- raw
	retransmittedResponse := receiveFragmentPacket(t, transport.sentIKE)
	if !bytes.Equal(retransmittedResponse, deleteResponse) {
		t.Fatal("retransmitted old IKE Delete did not reuse the authenticated response")
	}
}

func retiredIKEKeysCleared(session *Session, keys *IKEKeys) bool {
	session.mu.RLock()
	defer session.mu.RUnlock()
	return session.retiredIKESA == nil && allZero(keys.SK_d)
}

func cloneIKEKeysForTest(keys *IKEKeys) *IKEKeys {
	return &IKEKeys{
		SKEYSEED: append([]byte(nil), keys.SKEYSEED...),
		SK_d:     append([]byte(nil), keys.SK_d...), SK_ai: append([]byte(nil), keys.SK_ai...),
		SK_ar: append([]byte(nil), keys.SK_ar...), SK_ei: append([]byte(nil), keys.SK_ei...),
		SK_er: append([]byte(nil), keys.SK_er...), SK_pi: append([]byte(nil), keys.SK_pi...),
		SK_pr: append([]byte(nil), keys.SK_pr...),
	}
}

func respondToIKESARekey(t *testing.T, session *Session, transport *testIKETransport) {
	t.Helper()
	respondToIKESARekeyResponse(t, session, transport)
	respondToOldIKESADelete(t, session, transport)
}

func respondToIKESARekeyResponse(t *testing.T, session *Session, transport *testIKETransport) {
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
			_, peerKey, err = parseKEPayload(payload)
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
	proposals := buildIKEProposalsForSession(session)
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

}

func respondToOldIKESADelete(t *testing.T, session *Session, transport *testIKETransport) {
	t.Helper()
	deleteRaw := <-transport.sentIKE
	deleteRequest, _ := ikev2.DecodePacket(deleteRaw)
	deleteResponse := &ikev2.IKEPacket{
		InitiatorSPI: deleteRequest.InitiatorSPI, ResponderSPI: deleteRequest.ResponderSPI,
		Version: 0x20, ExchangeType: deleteRequest.ExchangeType,
		Flags: ikeResponseFlag, MessageID: deleteRequest.MessageID,
	}
	encoded, _ := session.encryptAndWrap(deleteResponse)
	transport.ike <- encoded
}
