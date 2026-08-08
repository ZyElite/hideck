package ipsec

import (
	"bytes"
	"testing"
	"time"
	"unsafe"

	enginecrypto "github.com/iniwex5/vowifi-go/engine/crypto"
)

const transportTestTimeout = 3 * time.Second

func TestLegacyRuntimeTypeOffsets(t *testing.T) {
	if unsafe.Sizeof(uintptr(0)) != 8 {
		t.Skip("legacy v1.5.5 binary layout is amd64")
	}
	var association SecurityAssociation
	if unsafe.Offsetof(association.SPI) != 16 || unsafe.Offsetof(association.EncryptionAlg) != 24 ||
		unsafe.Offsetof(association.preparedCipher) != 88 || unsafe.Offsetof(association.IsAEAD) != 176 {
		t.Fatal("security association legacy field offsets changed")
	}
	var socket SocketManager
	if unsafe.Offsetof(socket.DeviceID) != 32 || unsafe.Offsetof(socket.IKEChan) != 112 ||
		unsafe.Offsetof(socket.NetEvents) != 128 {
		t.Fatal("socket manager legacy field offsets changed")
	}
	var socks Socks5Transport
	if unsafe.Offsetof(socks.cfg) != 80 || unsafe.Offsetof(socks.ikeChan) != 296 ||
		unsafe.Offsetof(socks.stopOnce) != 360 {
		t.Fatal("SOCKS5 transport legacy field offsets changed")
	}
}

func TestLegacySecurityAssociationAPI(t *testing.T) {
	key := bytes.Repeat([]byte{0x31}, 20)
	encrypter, err := enginecrypto.GetEncrypterWithKeyLen(enginecrypto.EncrAESGCM16, 128)
	if err != nil {
		t.Fatalf("GetEncrypterWithKeyLen: %v", err)
	}
	sa := NewSecurityAssociation(0x01020304, encrypter, key, []byte("legacy-integrity-key"))
	if sa.SPI != 0x01020304 || sa.EncryptionAlg != encrypter || !sa.IsAEAD {
		t.Fatalf("legacy SA fields were not restored: %+v", sa)
	}
	if sa.preparedCipher == nil || sa.preparedCipherErr != nil {
		t.Fatalf("prepared cipher = %T, err = %v", sa.preparedCipher, sa.preparedCipherErr)
	}
	inner := fakeIPPacket(37)
	frame, err := Encapsulate(inner, sa)
	if err != nil {
		t.Fatalf("legacy Encapsulate: %v", err)
	}
	decoded, err := Decapsulate(frame, NewSecurityAssociation(0x01020304, encrypter, key, nil))
	if err != nil {
		t.Fatalf("legacy Decapsulate: %v", err)
	}
	if !bytes.Equal(decoded, inner) {
		t.Fatal("legacy ESP round trip changed the inner packet")
	}
}

func TestLegacyCBCAssociationAPI(t *testing.T) {
	key := bytes.Repeat([]byte{0x42}, 16)
	integKey := bytes.Repeat([]byte{0x24}, 20)
	encrypter, err := enginecrypto.GetEncrypterWithKeyLen(enginecrypto.EncrAESCBC, 128)
	if err != nil {
		t.Fatalf("GetEncrypterWithKeyLen: %v", err)
	}
	integrity, err := enginecrypto.GetIntegrityAlgorithm(2)
	if err != nil {
		t.Fatalf("GetIntegrityAlgorithm: %v", err)
	}
	sa := NewSecurityAssociationCBC(7, encrypter, key, integrity, integKey)
	if sa.IsAEAD || sa.IntegrityAlg2 != integrity || !bytes.Equal(sa.IntegrityKey, integKey) {
		t.Fatalf("legacy CBC fields were not restored: %+v", sa)
	}
}

func TestEncapsulateIntoPreservesDestinationPrefix(t *testing.T) {
	key := bytes.Repeat([]byte{0x73}, 20)
	sa := NewSecurityAssociation(11, enginecrypto.EncrAESGCM16, key, 0)
	prefix := []byte{0xde, 0xad, 0xbe, 0xef}
	inner := fakeIPPacket(23)
	frame, err := EncapsulateInto(append([]byte(nil), prefix...), inner, sa)
	if err != nil {
		t.Fatalf("EncapsulateInto: %v", err)
	}
	if !bytes.Equal(frame[:len(prefix)], prefix) {
		t.Fatalf("destination prefix = %x, want %x", frame[:len(prefix)], prefix)
	}
	decoded, _, err := DecapsulateWithNextHeaderInto(append([]byte(nil), prefix...), frame[len(prefix):], NewSecurityAssociation(11, enginecrypto.EncrAESGCM16, key, 0))
	if err != nil {
		t.Fatalf("DecapsulateWithNextHeaderInto: %v", err)
	}
	wantDecoded := append(append([]byte(nil), prefix...), inner...)
	if !bytes.Equal(decoded, wantDecoded) {
		t.Fatalf("decoded packet = %x, want %x", decoded, wantDecoded)
	}
}

func TestCBCEncapsulateIntoExcludesDestinationPrefixFromICV(t *testing.T) {
	key := bytes.Repeat([]byte{0x63}, 16)
	integKey := bytes.Repeat([]byte{0x36}, 20)
	integrity, err := enginecrypto.GetIntegrityAlgorithm(2)
	if err != nil {
		t.Fatalf("GetIntegrityAlgorithm: %v", err)
	}
	prefix := []byte{0xca, 0xfe}
	inner := fakeIPPacket(31)
	outbound := NewSecurityAssociationCBC(12, enginecrypto.EncrAESCBC, key, integrity, integKey)
	frame, err := EncapsulateInto(append([]byte(nil), prefix...), inner, outbound)
	if err != nil {
		t.Fatalf("EncapsulateInto: %v", err)
	}
	inbound := NewSecurityAssociationCBC(12, enginecrypto.EncrAESCBC, key, integrity, integKey)
	decoded, err := Decapsulate(frame[len(prefix):], inbound)
	if err != nil {
		t.Fatalf("Decapsulate: %v", err)
	}
	if !bytes.Equal(decoded, inner) {
		t.Fatalf("decoded packet = %x, want %x", decoded, inner)
	}
}

func TestSocketManagerOwnsReceivedBuffersAndReportsRebinding(t *testing.T) {
	server := newLoopbackSocketManager(t, "server", "127.0.0.1:500")
	defer server.Stop()
	client := newLoopbackSocketManager(t, "client", server.LocalAddrString())
	defer client.Stop()

	first := []byte{1, 2, 3, 4, 5, 6, 7, 8, 0xaa}
	second := []byte{9, 8, 7, 6, 5, 4, 3, 2, 0xbb}
	if err := client.SendESP(first); err != nil {
		t.Fatalf("send first ESP: %v", err)
	}
	if err := client.SendESP(second); err != nil {
		t.Fatalf("send second ESP: %v", err)
	}
	receivedFirst := receivePacket(t, server.ESPPackets())
	receivedSecond := receivePacket(t, server.ESPPackets())
	if !bytes.Equal(receivedFirst, first) || !bytes.Equal(receivedSecond, second) {
		t.Fatalf("received buffers were reused: %x %x", receivedFirst, receivedSecond)
	}
	event := receiveEvent(t, server.NetEventsChan())
	if event.Type != EventNATPortChanged || event.OldPort != 500 || event.NewPort != int(client.LocalPort()) {
		t.Fatalf("NAT rebinding event = %+v", event)
	}
	stats := server.Stats()
	if stats.ReceivedESP != 2 || stats.DroppedESP != 0 {
		t.Fatalf("socket stats = %+v", stats)
	}
}

func TestSocketManagerLifecycleIsIdempotent(t *testing.T) {
	manager, err := NewSocketManager("lifecycle", "127.0.0.1:0", "127.0.0.1:500", "")
	if err != nil {
		t.Fatalf("NewSocketManager: %v", err)
	}
	manager.Stop()
	manager.Stop()
	if err := manager.Start(); err == nil {
		t.Fatal("Start after Stop succeeded")
	}
	if err := manager.SendIKE(fakeIKEInit()); err == nil {
		t.Fatal("SendIKE after Stop succeeded")
	}
}

func TestExtendedSocketErrorEventMapping(t *testing.T) {
	event, ok := netEventFromExtendedError(linuxErrMessageSize, minimumReportedPMTU)
	if !ok || event.Type != EventPathMTU || event.PMTU != minimumReportedPMTU {
		t.Fatalf("minimum PMTU event = %+v, %t", event, ok)
	}
	if _, ok := netEventFromExtendedError(linuxErrMessageSize, minimumReportedPMTU-1); ok {
		t.Fatal("invalid low PMTU was accepted")
	}
	if _, ok := netEventFromExtendedError(linuxErrMessageSize, maximumReportedPMTU+1); ok {
		t.Fatal("invalid high PMTU was accepted")
	}
	event, ok = netEventFromExtendedError(linuxErrHostUnreachable, 0)
	if !ok || event.Type != EventNetworkDown || event.Reason == "" {
		t.Fatalf("unreachable event = %+v, %t", event, ok)
	}
}

func TestSocks5TransportEndToEnd(t *testing.T) {
	proxy := newTestSocks5Proxy(t)
	defer proxy.close()
	transport, err := NewSocks5Transport(Socks5Config{
		ProxyAddr: proxy.address(), RemoteAddr: "127.0.0.1:4500", DeviceID: "test-device",
	})
	if err != nil {
		t.Fatalf("NewSocks5Transport: %v", err)
	}
	if err := transport.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}

	ike := fakeIKEInit()
	if err := transport.SendIKE(ike); err != nil {
		t.Fatalf("SendIKE: %v", err)
	}
	wire, clientAddr := proxy.receive(t)
	datagram, err := DecodeSocks5UDPDatagram(wire)
	if err != nil {
		t.Fatalf("decode outbound datagram: %v", err)
	}
	if datagram.DstAddr.Port != 4500 || !bytes.Equal(datagram.Data[:4], []byte{0, 0, 0, 0}) {
		t.Fatalf("outbound SOCKS5 IKE datagram = %+v %x", datagram.DstAddr, datagram.Data)
	}

	proxy.send(t, clientAddr, EncodeSocks5UDPDatagram(datagram.DstAddr, datagram.Data))
	if received := receivePacket(t, transport.IKEPackets()); !bytes.Equal(received, ike) {
		t.Fatalf("received IKE = %x, want %x", received, ike)
	}
	esp := []byte{1, 2, 3, 4, 0, 0, 0, 1, 0xaa}
	proxy.send(t, clientAddr, EncodeSocks5UDPDatagram(datagram.DstAddr, esp))
	if received := receivePacket(t, transport.ESPPackets()); !bytes.Equal(received, esp) {
		t.Fatalf("received ESP = %x, want %x", received, esp)
	}
	proxy.send(t, clientAddr, EncodeSocks5UDPDatagram(datagram.DstAddr, []byte{0xff}))
	waitForCounter(t, func() uint64 { return transport.SnapshotStats().NATKeepaliveDrop })

	stats := transport.SnapshotStats()
	if stats.ReceivedIKETotal != 1 || stats.ReceivedESPTotal != 1 || stats.LastESPReadLen != uint64(len(esp)) {
		t.Fatalf("SOCKS5 stats = %+v", stats)
	}
	transport.Stop()
	transport.Stop()
	if err := transport.Start(); err == nil {
		t.Fatal("SOCKS5 Start after Stop succeeded")
	}
	if err := transport.SendESP(esp); err == nil {
		t.Fatal("SOCKS5 SendESP after Stop succeeded")
	}
}

func newLoopbackSocketManager(t *testing.T, deviceID, remote string) *SocketManager {
	t.Helper()
	manager, err := NewSocketManager(deviceID, "127.0.0.1:0", remote, "")
	if err != nil {
		t.Fatalf("NewSocketManager(%s): %v", deviceID, err)
	}
	if err := manager.Start(); err != nil {
		manager.Stop()
		t.Fatalf("Start(%s): %v", deviceID, err)
	}
	return manager
}

func receivePacket(t *testing.T, packets <-chan []byte) []byte {
	t.Helper()
	select {
	case packet := <-packets:
		return packet
	case <-time.After(transportTestTimeout):
		t.Fatal("timed out waiting for packet")
		return nil
	}
}

func receiveEvent(t *testing.T, events <-chan NetEvent) NetEvent {
	t.Helper()
	select {
	case event := <-events:
		return event
	case <-time.After(transportTestTimeout):
		t.Fatal("timed out waiting for network event")
		return NetEvent{}
	}
}

func waitForCounter(t *testing.T, load func() uint64) {
	t.Helper()
	deadline := time.Now().Add(transportTestTimeout)
	for load() == 0 {
		if time.Now().After(deadline) {
			t.Fatal("timed out waiting for counter")
		}
		time.Sleep(time.Millisecond)
	}
}
