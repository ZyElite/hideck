package ipsec

import (
	"bytes"
	"crypto/aes"
	"encoding/binary"
	"net"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/iniwex5/vowifi-go/engine/crypto"
)

// fakeIPPacket builds a minimal 20-byte IPv4 header + payload.
func fakeIPPacket(n int) []byte {
	pkt := make([]byte, 20+n)
	pkt[0] = 0x45 // IPv4, IHL=5
	binary.BigEndian.PutUint16(pkt[2:4], uint16(len(pkt)))
	pkt[9] = 17 // UDP
	copy(pkt[12:16], []byte{10, 0, 0, 2})
	copy(pkt[16:20], []byte{10, 0, 0, 1})
	for i := 20; i < len(pkt); i++ {
		pkt[i] = byte(i)
	}
	return pkt
}

// fakeIKEInit builds a minimal IKE_SA_INIT packet (RFC 7296 header).
func fakeIKEInit() []byte {
	pkt := make([]byte, 28)
	copy(pkt[0:8], []byte{1, 2, 3, 4, 5, 6, 7, 8}) // initiator SPI
	pkt[16] = 0                                    // next payload: none
	pkt[17] = 0x20                                 // IKEv2
	pkt[18] = 34                                   // IKE_SA_INIT
	pkt[19] = 0x08                                 // initiator flag
	binary.BigEndian.PutUint32(pkt[24:28], 28)
	return pkt
}

func TestESPEncapsulateDecapsulateGCM(t *testing.T) {
	key := bytes.Repeat([]byte{0x33}, 20) // 16-byte AES key + 4-byte salt (RFC 4106)
	sa := NewSecurityAssociation(0x11223344, crypto.EncrAESGCM16, key, 0)
	if sa.NextSequenceNumber() != 1 {
		t.Error("first sequence number must be 1")
	}

	inner := fakeIPPacket(80)
	total, _, err := EncapsulationLayout(len(inner), sa)
	if err != nil {
		t.Fatalf("EncapsulationLayout: %v", err)
	}
	enc, err := Encapsulate(inner, nil, sa)
	if err != nil {
		t.Fatalf("Encapsulate: %v", err)
	}
	if len(enc) != total {
		t.Errorf("frame length = %d, want %d", len(enc), total)
	}
	// SPI is in clear at the front.
	if spi := binary.BigEndian.Uint32(enc[0:4]); spi != 0x11223344 {
		t.Errorf("SPI = %x, want 11223344", spi)
	}

	dec, err := Decapsulate(enc, nil, sa)
	if err != nil {
		t.Fatalf("Decapsulate: %v", err)
	}
	if !bytes.Equal(dec, inner) {
		t.Error("decapsulated packet differs from the original")
	}

	// Tampering must be detected (GCM authenticates).
	bad := append([]byte{}, enc...)
	bad[len(bad)-1] ^= 0x01
	if _, err := Decapsulate(bad, nil, sa); err == nil {
		t.Error("Decapsulate accepted a tampered frame")
	}

	// SPI mismatch must be rejected.
	other := NewSecurityAssociation(0xdeadbeef, crypto.EncrAESGCM16, key, 0)
	if _, err := Decapsulate(enc, nil, other); err == nil {
		t.Error("Decapsulate accepted a frame for a different SPI")
	}
}

func TestESPEncapsulateDecapsulateCBC(t *testing.T) {
	key := bytes.Repeat([]byte{0x11}, 16) // AES-128
	integ := crypto.NewIntegrity(2)       // HMAC-SHA1-96
	integKey := bytes.Repeat([]byte{0x22}, 20)
	sa := NewSecurityAssociationCBC(0x55667788, crypto.EncrAESCBC, key, integ, integKey, 0)

	inner := fakeIPPacket(64)
	total, _, err := EncapsulationLayout(len(inner), sa)
	if err != nil {
		t.Fatalf("EncapsulationLayout: %v", err)
	}
	enc, err := Encapsulate(inner, nil, sa)
	if err != nil {
		t.Fatalf("Encapsulate: %v", err)
	}
	if len(enc) != total {
		t.Errorf("frame length = %d, want %d", len(enc), total)
	}
	// The CBC ciphertext portion must be block aligned (header/ICV are not).
	if (len(enc) - 8 - 16 - 12)%aes.BlockSize != 0 {
		t.Errorf("CBC ciphertext not block aligned: %d", len(enc))
	}

	dec, nh, err := DecapsulateWithNextHeaderInto(nil, enc, sa)
	if err != nil {
		t.Fatalf("DecapsulateWithNextHeaderInto: %v", err)
	}
	if nh != 4 {
		t.Errorf("next header = %d, want 4 (IPv4)", nh)
	}
	if !bytes.Equal(dec, inner) {
		t.Error("decapsulated packet differs from the original")
	}

	// Tampered ICV must be rejected.
	bad := append([]byte{}, enc...)
	bad[len(bad)-1] ^= 0x01
	if _, err := Decapsulate(bad, nil, sa); err == nil {
		t.Error("Decapsulate accepted a frame with a bad ICV")
	}
}

func TestEncapsulationLayout(t *testing.T) {
	key := bytes.Repeat([]byte{0x33}, 20)
	sa := NewSecurityAssociation(1, crypto.EncrAESGCM16, key, 0)
	total, pad, err := EncapsulationLayout(20, sa)
	if err != nil {
		t.Fatalf("EncapsulationLayout: %v", err)
	}
	// padding = (16 - 22 % 16) % 16 = 10; total = 8 + 8 + 20 + 2 + 10 + 16.
	if pad != 10 {
		t.Errorf("padding = %d, want 10", pad)
	}
	if total != 64 {
		t.Errorf("total = %d, want 64", total)
	}
}

func TestParseIKEPayload(t *testing.T) {
	ike := fakeIKEInit()
	if pkt, ok := parseIKEPayload(ike, len(ike)); !ok || !bytes.Equal(pkt, ike) {
		t.Error("plain IKE packet not classified as IKE")
	}

	// Non-ESP marker (RFC 3948): 4 zero bytes + IKE packet.
	marked := append([]byte{0, 0, 0, 0}, ike...)
	if pkt, ok := parseIKEPayload(marked, len(marked)); !ok || !bytes.Equal(pkt, ike) {
		t.Error("marked IKE packet not classified correctly")
	}

	// ESP frame: arbitrary bytes with a non-zero SPI.
	esp := make([]byte, 40)
	copy(esp, []byte{0xaa, 0xbb, 0xcc, 0xdd})
	if _, ok := parseIKEPayload(esp, len(esp)); ok {
		t.Error("ESP frame misclassified as IKE")
	}

	// NAT keepalive: single 0xff.
	if _, ok := parseIKEPayload([]byte{0xff}, 1); ok {
		t.Error("NAT keepalive misclassified as IKE")
	}
}

func TestSocks5UDPDatagram(t *testing.T) {
	addr := &net.UDPAddr{IP: net.ParseIP("10.0.0.1"), Port: 4500}
	data := []byte("esp payload bytes")
	dgram := EncodeSocks5UDPDatagram(addr, data)
	if len(dgram) != 10+len(data) {
		t.Fatalf("datagram length = %d, want %d", len(dgram), 10+len(data))
	}
	dec, err := DecodeSocks5UDPDatagram(dgram)
	if err != nil {
		t.Fatalf("DecodeSocks5UDPDatagram: %v", err)
	}
	if !dec.Addr.IP.Equal(net.ParseIP("10.0.0.1")) || dec.Addr.Port != 4500 {
		t.Errorf("decoded addr = %v, want 10.0.0.1:4500", dec.Addr)
	}
	if !bytes.Equal(dec.Data, data) {
		t.Error("decoded data differs")
	}
	if dec.Frag != 0 {
		t.Errorf("frag = %d, want 0", dec.Frag)
	}

	// IPv6.
	addr6 := &net.UDPAddr{IP: net.ParseIP("2001:db8::1"), Port: 4500}
	dgram6 := EncodeSocks5UDPDatagram(addr6, data)
	if len(dgram6) != 22+len(data) {
		t.Fatalf("IPv6 datagram length = %d, want %d", len(dgram6), 22+len(data))
	}
	dec6, err := DecodeSocks5UDPDatagram(dgram6)
	if err != nil {
		t.Fatalf("Decode IPv6 datagram: %v", err)
	}
	if !dec6.Addr.IP.Equal(net.ParseIP("2001:db8::1")) {
		t.Errorf("decoded IPv6 addr = %v", dec6.Addr.IP)
	}

	// Truncated datagrams must error.
	if _, err := DecodeSocks5UDPDatagram(dgram[:9]); err == nil {
		t.Error("truncated datagram accepted")
	}
}

// mockRW is a duplex mock used for SOCKS5 handshake tests.
type mockRW struct {
	r *bytes.Buffer
	w *bytes.Buffer
}

func (m *mockRW) Read(p []byte) (int, error)  { return m.r.Read(p) }
func (m *mockRW) Write(p []byte) (int, error) { return m.w.Write(p) }

func TestSocks5HandshakeNoAuth(t *testing.T) {
	m := &mockRW{r: bytes.NewBuffer([]byte{5, 0}), w: &bytes.Buffer{}}
	if err := socks5Handshake(m, nil); err != nil {
		t.Fatalf("socks5Handshake: %v", err)
	}
	if got := m.w.Bytes(); !bytes.Equal(got, []byte{5, 1, 0}) {
		t.Errorf("greeting = %x, want [05 01 00]", got)
	}
}

func TestSocks5HandshakeUserPass(t *testing.T) {
	m := &mockRW{
		r: bytes.NewBuffer([]byte{5, 2, 1, 0}),
		w: &bytes.Buffer{},
	}
	cfg := &Socks5Config{Username: "user", Password: "pass"}
	if err := socks5Handshake(m, cfg); err != nil {
		t.Fatalf("socks5Handshake: %v", err)
	}
	want := []byte{5, 2, 0, 2, 1, 4, 'u', 's', 'e', 'r', 4, 'p', 'a', 's', 's'}
	if !bytes.Equal(m.w.Bytes(), want) {
		t.Errorf("greeting+auth = %x, want %x", m.w.Bytes(), want)
	}
}

func TestSocks5ReplyString(t *testing.T) {
	cases := map[byte]string{
		0: "succeeded", 1: "general failure", 4: "host unreachable",
		5: "connection refused", 8: "address type not supported",
	}
	for code, want := range cases {
		if got := socks5ReplyString(code); got != want {
			t.Errorf("socks5ReplyString(%d) = %q, want %q", code, got, want)
		}
	}
	if !strings.Contains(socks5ReplyString(99), "99") {
		t.Error("unknown reply code should include the number")
	}
}

func TestReadSocks5Reply(t *testing.T) {
	// Relay reply: 10.0.0.1:4500.
	m := &mockRW{r: bytes.NewBuffer([]byte{5, 0, 0, 1, 10, 0, 0, 1, 0x11, 0x94}), w: &bytes.Buffer{}}
	addr, err := readSocks5Reply(m)
	if err != nil {
		t.Fatalf("readSocks5Reply: %v", err)
	}
	if !addr.IP.Equal(net.ParseIP("10.0.0.1")) || addr.Port != 4500 {
		t.Errorf("relay addr = %v, want 10.0.0.1:4500", addr)
	}

	// Failure reply.
	m2 := &mockRW{r: bytes.NewBuffer([]byte{5, 5, 0, 0}), w: &bytes.Buffer{}}
	if _, err := readSocks5Reply(m2); err == nil || !strings.Contains(err.Error(), "connection refused") {
		t.Errorf("failure reply error = %v", err)
	}
}

func TestBuildSocks5Request(t *testing.T) {
	ipv4 := net.ParseIP("10.0.0.1")
	req := buildSocks5Request(socks5CmdUDPAssociate, ipv4, 4500)
	if !bytes.Equal(req, []byte{5, 3, 0, 1, 10, 0, 0, 1, 0x11, 0x94}) {
		t.Errorf("IPv4 request = %x", req)
	}

	// IPv4-mapped IPv6 must collapse to ATYP=1.
	mapped := net.ParseIP("::ffff:10.0.0.1")
	req2 := buildSocks5Request(socks5CmdConnect, mapped, 1080)
	if req2[3] != 1 || !bytes.Equal(req2[4:8], []byte{10, 0, 0, 1}) {
		t.Errorf("mapped request = %x", req2)
	}

	// IPv6.
	ipv6 := net.ParseIP("2001:db8::1")
	req3 := buildSocks5Request(socks5CmdUDPAssociate, ipv6, 4500)
	if req3[3] != 4 || len(req3) != 22 {
		t.Errorf("IPv6 request = %x (len %d)", req3, len(req3))
	}
}

func TestParseSocks5Addr(t *testing.T) {
	host, port, err := parseSocks5Addr("proxy.example.com")
	if err != nil || host != "proxy.example.com" || port != 1080 {
		t.Errorf("bare host = %q:%d err %v, want host:1080", host, port, err)
	}
	host, port, err = parseSocks5Addr("proxy:4500")
	if err != nil || host != "proxy" || port != 4500 {
		t.Errorf("host:port = %q:%d err %v", host, port, err)
	}
	if _, _, err := parseSocks5Addr("proxy:bad"); err == nil {
		t.Error("invalid port accepted")
	}
}

func TestResolveUDPAddrAll(t *testing.T) {
	addrs, err := ResolveUDPAddrAll("127.0.0.1", "4500")
	if err != nil {
		t.Fatalf("ResolveUDPAddrAll: %v", err)
	}
	if len(addrs) != 1 || !addrs[0].IP.Equal(net.ParseIP("127.0.0.1")) || addrs[0].Port != 4500 {
		t.Errorf("addrs = %v, want [127.0.0.1:4500]", addrs)
	}

	// Combined host:port form.
	addrs, err = ResolveUDPAddrAll("127.0.0.1:500", "")
	if err != nil || len(addrs) != 1 || addrs[0].Port != 500 {
		t.Errorf("combined form: %v %v", addrs, err)
	}

	// IPv4-mapped IPv6 collapses to IPv4.
	addrs, err = ResolveUDPAddrAll("::ffff:127.0.0.1", "500")
	if err != nil || len(addrs) != 1 || len(addrs[0].IP) != 4 {
		t.Errorf("mapped addr: %v %v", addrs, err)
	}

	if _, err := ResolveUDPAddrAll("127.0.0.1", "bogus-service-name"); err == nil {
		t.Error("bad port accepted")
	}
}

// TestSocketManagerLoopback drives an ESP packet end-to-end between two
// SocketManagers over loopback.
func TestSocketManagerLoopback(t *testing.T) {
	server, err := NewSocketManager("127.0.0.1", "127.0.0.1:0", "127.0.0.1", "4500")
	if err != nil {
		t.Fatalf("server: %v", err)
	}
	client, err := NewSocketManager("127.0.0.1", "127.0.0.1:0", "127.0.0.1", strconv.Itoa(int(server.LocalPort())))
	if err != nil {
		t.Fatalf("client: %v", err)
	}
	server.Start()
	client.Start()
	defer server.Stop()
	defer client.Stop()

	key := bytes.Repeat([]byte{0x33}, 20)
	sa := NewSecurityAssociation(0x10203040, crypto.EncrAESGCM16, key, 0)
	inner := fakeIPPacket(64)
	enc, err := Encapsulate(inner, nil, sa)
	if err != nil {
		t.Fatalf("Encapsulate: %v", err)
	}

	client.SendESP(enc)
	select {
	case got := <-server.ESPPackets():
		dec, err := Decapsulate(got, nil, sa)
		if err != nil {
			t.Fatalf("server Decapsulate: %v", err)
		}
		if !bytes.Equal(dec, inner) {
			t.Error("decapsulated packet differs")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for the ESP packet")
	}

	// IKE path (with the RFC 3948 marker on port 4500).
	ike := fakeIKEInit()
	client.SendIKE(ike)
	select {
	case got := <-server.IKEPackets():
		if !bytes.Equal(got, ike) {
			t.Error("IKE packet differs after marker stripping")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for the IKE packet")
	}
}

// TestSocks5HandshakeFailure exercises the error paths.
func TestSocks5HandshakeFailure(t *testing.T) {
	// No acceptable methods.
	m := &mockRW{r: bytes.NewBuffer([]byte{5, 0xff}), w: &bytes.Buffer{}}
	if err := socks5Handshake(m, nil); err == nil {
		t.Error("0xff method should error")
	}
	// Wrong version.
	m2 := &mockRW{r: bytes.NewBuffer([]byte{4, 0}), w: &bytes.Buffer{}}
	if err := socks5Handshake(m2, nil); err == nil {
		t.Error("bad version should error")
	}
}
