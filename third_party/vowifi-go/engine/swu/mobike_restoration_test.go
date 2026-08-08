package swu

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net"
	"testing"
	"time"

	enginecrypto "github.com/iniwex5/vowifi-go/engine/crypto"
	"github.com/iniwex5/vowifi-go/engine/driver"
	"github.com/iniwex5/vowifi-go/engine/ikev2"
	"github.com/iniwex5/vowifi-go/engine/ipsec"
)

var (
	_ func(*Session) ([]byte, error)                                               = (*Session).sendMOBIKEUpdate
	_ func(*Session, []byte, []byte) error                                         = (*Session).verifyCookie2Response
	_ func(*Session, string, string) error                                         = (*Session).updateXFRMState
	_ func(*Session, *driver.XFRMSAConfig, *ipsec.SecurityAssociation, bool) error = (*Session).fillSAKeys
	_ func(*Session)                                                               = (*Session).startNetEventMonitor
)

func compileMOBIKEUpdateCallShapes(session *Session, oldIP, newIP net.IP) {
	_ = session.UpdateAddresses("192.0.2.10", "198.51.100.20")
	_ = session.UpdateAddresses(oldIP, newIP)
}

func TestMOBIKENegotiationTracksResponderSupport(t *testing.T) {
	session := NewSession(&Config{})
	session.applyMOBIKENegotiation([]ikev2.Payload{
		&ikev2.EncryptedPayloadNotify{NotifyType: ikev2.MOBIKE_SUPPORTED},
	})
	if !session.mobikeSupported {
		t.Fatal("MOBIKE_SUPPORTED did not enable address updates")
	}
}

func TestMOBIKEInterimAddressShapeKeepsRemoteEndpoint(t *testing.T) {
	session := NewSession(&Config{})
	transport := newTestIKETransport()
	setMOBIKETuple(transport, "192.0.2.10", "198.51.100.10")
	session.setTransport(transport)
	spec, err := session.parseMOBIKEAddresses([]any{
		net.ParseIP("192.0.2.10"), net.ParseIP("192.0.2.20"),
	})
	if err != nil {
		t.Fatalf("parse current MOBIKE call shape: %v", err)
	}
	if spec.localHost != "192.0.2.20" || spec.remoteHost != "198.51.100.10" {
		t.Fatalf("current MOBIKE endpoints = %#v", spec)
	}
}

func TestFillSAKeysRestoresAEADMapping(t *testing.T) {
	session := NewSession(&Config{})
	session.espCipher, session.espEncKeyBits = enginecrypto.EncrAESGCM16, 128
	key := bytes.Repeat([]byte{0x41}, 20)
	config := &driver.XFRMSAConfig{}
	if err := session.fillSAKeys(config, &ipsec.SecurityAssociation{EncryptionKey: key}, true); err != nil {
		t.Fatalf("fillSAKeys: %v", err)
	}
	if !config.IsAEAD || config.AeadAlgoName == "" || !bytes.Equal(config.AeadKey, key) || config.AeadICVLen != 128 {
		t.Fatalf("AEAD XFRM config = %#v", config)
	}
}

func TestMOBIKEUpdateMigratesProductionControlPlane(t *testing.T) {
	session, previous := newEstablishedControlSession(t)
	defer session.Shutdown()
	setMOBIKETuple(previous, "192.0.2.10", "198.51.100.10")
	previous.closeOnStop = true
	if err := session.startEstablishedDataPlane(); err != nil {
		t.Fatalf("start data plane: %v", err)
	}
	replacement := newTestIKETransport()
	setMOBIKETuple(replacement, "192.0.2.20", "198.51.100.20")
	var localBind, remoteBind string
	session.cfg.TransportFactory = func(local, remote string) (Transport, error) {
		localBind, remoteBind = local, remote
		return replacement, nil
	}
	session.mobikeSupported = true
	peerResult := make(chan error, 1)
	go func() { peerResult <- answerMOBIKEUpdate(session, previous) }()

	if err := session.UpdateAddresses("192.0.2.20", "198.51.100.20"); err != nil {
		t.Fatalf("UpdateAddresses: %v", err)
	}
	if err := <-peerResult; err != nil {
		t.Fatal(err)
	}
	if session.transport() != replacement || !previous.stopped.Load() || !replacement.started.Load() {
		t.Fatal("MOBIKE did not atomically replace and stop transports")
	}
	if localBind != "192.0.2.20:0" || remoteBind != "198.51.100.20:4500" {
		t.Fatalf("factory endpoints = %q %q", localBind, remoteBind)
	}
	go answerEmptyInformational(session, replacement)
	if err := session.DPDProbe(); err != nil {
		t.Fatalf("DPD on migrated control plane: %v", err)
	}
	assertMigratedUserspaceDataPlane(t, session, replacement)
}

func assertMigratedUserspaceDataPlane(t *testing.T, session *Session, transport *testIKETransport) {
	t.Helper()
	outbound := testIPv4Flow(session.innerIP, net.IPv4(8, 8, 8, 8))
	if err := session.innerEndpoint.WritePacket(context.Background(), outbound); err != nil {
		t.Fatalf("write migrated outbound packet: %v", err)
	}
	select {
	case <-transport.sentESP:
	case <-time.After(time.Second):
		t.Fatal("migrated transport did not carry outbound ESP")
	}
	inbound := testIPv4Flow(net.IPv4(8, 8, 8, 8), session.innerIP)
	esp, err := ipsec.Encapsulate(inbound, session.espInboundSA)
	if err != nil {
		t.Fatalf("encapsulate migrated inbound packet: %v", err)
	}
	transport.esp <- esp
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	read, err := session.innerEndpoint.ReadPacket(ctx)
	if err != nil || !bytes.Equal(read, inbound) {
		t.Fatalf("migrated inbound packet = %x err=%v", read, err)
	}
}

func TestMOBIKEKernelFailureRestoresOldControlPlane(t *testing.T) {
	session, previous := newEstablishedControlSession(t)
	defer session.Shutdown()
	setMOBIKETuple(previous, "192.0.2.10", "198.51.100.10")
	replacement := newTestIKETransport()
	setMOBIKETuple(replacement, "192.0.2.20", "198.51.100.20")
	session.cfg.TransportFactory = func(string, string) (Transport, error) { return replacement, nil }
	session.mobikeSupported = true
	sentinel := errors.New("kernel MOBIKE transaction failed")
	session.kernelDataPlane = &rejectingMOBIKEPlane{err: sentinel}
	peerResult := make(chan error, 1)
	go func() { peerResult <- answerMOBIKEUpdate(session, previous) }()

	err := session.UpdateAddresses("192.0.2.20", "198.51.100.20")
	if !errors.Is(err, sentinel) {
		t.Fatalf("UpdateAddresses error = %v", err)
	}
	if peerErr := <-peerResult; peerErr != nil {
		t.Fatal(peerErr)
	}
	if session.transport() != previous || !replacement.stopped.Load() {
		t.Fatal("failed kernel migration did not restore the old transport")
	}
	go answerEmptyInformational(session, previous)
	if err := session.DPDProbe(); err != nil {
		t.Fatalf("DPD on restored control plane: %v", err)
	}
}

func TestPeerMOBIKEUpdateEchoesCookieThroughDispatcher(t *testing.T) {
	session, transport := newEstablishedControlSession(t)
	defer session.Shutdown()
	session.mobikeSupported = true
	cookie := bytes.Repeat([]byte{0x5a}, cookie2Size)
	request := &ikev2.IKEPacket{Header: newIKEHeader(
		session.SPIi, session.SPIr, ikev2.INFORMATIONAL, 0, 19,
	), Payloads: []ikev2.Payload{
		&ikev2.EncryptedPayloadNotify{NotifyType: ikev2.UPDATE_SA_ADDRESSES},
		&ikev2.EncryptedPayloadNotify{NotifyType: ikev2.COOKIE2, NotifyData: cookie},
	}}
	raw, err := session.encryptAndWrap(request)
	if err != nil {
		t.Fatalf("encrypt peer MOBIKE request: %v", err)
	}
	transport.ike <- raw
	response := receiveFragmentPacket(t, transport.sentIKE)
	packet, err := ikev2.DecodePacket(response)
	if err != nil {
		t.Fatalf("decode peer MOBIKE response: %v", err)
	}
	payloads, err := session.decryptAndParse(packet)
	if err != nil || len(payloads) != 1 {
		t.Fatalf("peer MOBIKE response payloads=%d err=%v", len(payloads), err)
	}
	notify, ok := payloads[0].(*ikev2.EncryptedPayloadNotify)
	if !ok || notify.NotifyType != ikev2.COOKIE2 || !bytes.Equal(notify.NotifyData, cookie) {
		t.Fatalf("peer MOBIKE response = %#v", payloads[0])
	}
}

func TestVerifyCookie2ResponseRejectsTampering(t *testing.T) {
	session, _ := newEstablishedControlSession(t)
	defer session.Shutdown()
	expected := bytes.Repeat([]byte{0x31}, cookie2Size)
	if err := session.verifyCookie2Response(nil, expected); err != nil {
		t.Fatalf("empty response: %v", err)
	}
	missing, err := session.encryptAndWrap(&ikev2.IKEPacket{Header: newIKEHeader(
		session.SPIi, session.SPIr, ikev2.INFORMATIONAL, ikeResponseFlag, 6,
	)})
	if err != nil || session.verifyCookie2Response(missing, expected) != nil {
		t.Fatalf("missing COOKIE2 response err=%v", err)
	}
	for name, cookie := range map[string][]byte{
		"match": expected, "mismatch": bytes.Repeat([]byte{0x32}, cookie2Size), "length": expected[:8],
	} {
		raw := protectedMOBIKEResponse(t, session, cookie)
		err := session.verifyCookie2Response(raw, expected)
		if (name == "match") != (err == nil) {
			t.Fatalf("%s COOKIE2 error = %v", name, err)
		}
	}
}

func protectedMOBIKEResponse(t *testing.T, session *Session, cookie []byte) []byte {
	t.Helper()
	packet := &ikev2.IKEPacket{Header: newIKEHeader(
		session.SPIi, session.SPIr, ikev2.INFORMATIONAL, ikeResponseFlag, 7,
	), Payloads: []ikev2.Payload{
		&ikev2.EncryptedPayloadNotify{NotifyType: ikev2.COOKIE2, NotifyData: append([]byte(nil), cookie...)},
	}}
	raw, err := session.encryptAndWrap(packet)
	if err != nil {
		t.Fatalf("encrypt MOBIKE response: %v", err)
	}
	return raw
}

func answerMOBIKEUpdate(session *Session, transport *testIKETransport) error {
	raw, err := receiveIKEForMOBIKE(transport.sentIKE)
	if err != nil {
		return err
	}
	request, err := ikev2.DecodePacket(raw)
	if err != nil {
		return err
	}
	payloads, err := session.decryptAndParse(request)
	if err != nil || len(payloads) != 4 {
		return fmt.Errorf("MOBIKE request payloads=%d err=%v", len(payloads), err)
	}
	update, updateOK := payloads[0].(*ikev2.EncryptedPayloadNotify)
	cookie, cookieOK := payloads[1].(*ikev2.EncryptedPayloadNotify)
	if !updateOK || update.NotifyType != ikev2.UPDATE_SA_ADDRESSES ||
		update.ProtocolID != 0 || !cookieOK || cookie.NotifyType != ikev2.COOKIE2 ||
		cookie.ProtocolID != 0 || len(cookie.NotifyData) != cookie2Size {
		return errors.New("MOBIKE request payload order is invalid")
	}
	source, sourceOK := payloads[2].(*ikev2.EncryptedPayloadNotify)
	destination, destinationOK := payloads[3].(*ikev2.EncryptedPayloadNotify)
	wantSource := natDetectionHash(session.SPIi, session.SPIr, transport.LocalIP(), transport.LocalPort())
	wantDestination := natDetectionHash(
		session.SPIi, session.SPIr, transport.RemoteIP(), uint16(transport.RemotePort()),
	)
	if !sourceOK || source.NotifyType != ikev2.NAT_DETECTION_SOURCE_IP ||
		!bytes.Equal(source.NotifyData, wantSource) || !destinationOK ||
		destination.NotifyType != ikev2.NAT_DETECTION_DESTINATION_IP ||
		!bytes.Equal(destination.NotifyData, wantDestination) {
		return errors.New("MOBIKE NAT detection payloads are invalid")
	}
	response := &ikev2.IKEPacket{Header: newIKEHeader(
		request.InitiatorSPI, request.ResponderSPI, request.ExchangeType, ikeResponseFlag, request.MessageID,
	), Payloads: []ikev2.Payload{
		&ikev2.EncryptedPayloadNotify{NotifyType: ikev2.COOKIE2, NotifyData: cookie.NotifyData},
	}}
	encoded, err := session.encryptAndWrap(response)
	if err == nil {
		transport.ike <- encoded
	}
	return err
}

func answerEmptyInformational(session *Session, transport *testIKETransport) {
	raw := <-transport.sentIKE
	request, _ := ikev2.DecodePacket(raw)
	response := &ikev2.IKEPacket{Header: newIKEHeader(
		request.InitiatorSPI, request.ResponderSPI, request.ExchangeType, ikeResponseFlag, request.MessageID,
	)}
	encoded, _ := session.encryptAndWrap(response)
	transport.ike <- encoded
}

func receiveIKEForMOBIKE(packets <-chan []byte) ([]byte, error) {
	select {
	case packet := <-packets:
		return packet, nil
	case <-time.After(time.Second):
		return nil, errors.New("timed out waiting for MOBIKE request")
	}
}

func setMOBIKETuple(transport *testIKETransport, local, remote string) {
	transport.localIP, transport.remoteIP = net.ParseIP(local), net.ParseIP(remote)
	transport.localPort, transport.remotePort = 4500, 4500
}

type rejectingMOBIKEPlane struct{ err error }

func (*rejectingMOBIKEPlane) Close() error                                { return nil }
func (*rejectingMOBIKEPlane) DeviceName() string                          { return "test-xfrm" }
func (*rejectingMOBIKEPlane) EnsureIPv6Enabled() error                    { return nil }
func (plane *rejectingMOBIKEPlane) Rekey(*Session, *childSARuntime) error { return plane.err }
func (plane *rejectingMOBIKEPlane) UpdateOuterAddresses(*Session, xfrmOuterTuple) error {
	return plane.err
}
