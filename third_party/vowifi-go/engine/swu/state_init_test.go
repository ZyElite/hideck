package swu

import (
	"net"
	"testing"

	"github.com/iniwex5/vowifi-go/engine/crypto"
	"github.com/iniwex5/vowifi-go/engine/ikev2"
)

func TestParseRedirectData(t *testing.T) {
	// IPv4.
	got, err := ParseRedirectData([]byte{1, 10, 0, 0, 1})
	if err != nil || got != "10.0.0.1" {
		t.Errorf("IPv4 redirect = %q err %v", got, err)
	}
	// IPv6.
	ip6 := net.ParseIP("2001:db8::1").To16()
	data := append([]byte{2}, ip6...)
	got, err = ParseRedirectData(data)
	if err != nil || got != "2001:db8::1" {
		t.Errorf("IPv6 redirect = %q err %v", got, err)
	}
	// FQDN.
	got, err = ParseRedirectData([]byte{3, 'e', 'p', 'd', 'g', '.', 'x'})
	if err != nil || got != "epdg.x" {
		t.Errorf("FQDN redirect = %q err %v", got, err)
	}
	// Errors.
	if _, err := ParseRedirectData(nil); err == nil {
		t.Error("empty redirect should error")
	}
	if _, err := ParseRedirectData([]byte{1, 10, 0, 0}); err == nil {
		t.Error("short IPv4 redirect should error")
	}
	if _, err := ParseRedirectData([]byte{9}); err == nil {
		t.Error("unknown redirect type should error")
	}
}

func newInitSession(t *testing.T) *Session {
	t.Helper()
	dh, err := crypto.NewDiffieHellman(14) // MODP-2048
	if err != nil {
		t.Fatalf("NewDiffieHellman: %v", err)
	}
	return &Session{
		cfg: &Config{
			IKEEncryption: crypto.EncrAESCBC, IKEEncryptionKeyBits: 128,
			IKEPRF: 2, IKEIntegrity: 2, IKEDH: 14,
			ESPEncryption: crypto.EncrAESCBC, ESPEncryptionKeyBits: 128, ESPIntegrity: 2,
		},
		dh:          dh,
		dhGroup:     14,
		encrAlg:     crypto.EncrAESCBC, // 12
		prfAlg:      2,                 // PRF_HMAC_SHA1
		integAlg:    2,                 // HMAC-SHA1-96
		nonceLen:    32,
		prf:         crypto.NewPRF(2),
		integKeyLen: 12,
		encKeyLen:   16,
		encKeyBits:  128,
	}
}

func TestBuildIKESAInitPacketCarriesAES256KeyLength(t *testing.T) {
	s := NewSession(&Config{
		IKEEncryption: crypto.EncrAESCBC, IKEEncryptionKeyBits: 256,
		IKEPRF: 7, IKEIntegrity: 14, IKEDH: 14,
		ESPEncryption: crypto.EncrAESCBC, ESPEncryptionKeyBits: 256, ESPIntegrity: 14,
	})
	raw, err := s.buildIKESAInitPacket()
	if err != nil {
		t.Fatalf("buildIKESAInitPacket() error = %v", err)
	}
	packet, err := ikev2.DecodePacket(raw)
	if err != nil {
		t.Fatal(err)
	}
	sa := packet.Payloads[0].(*ikev2.EncryptedPayloadSA)
	encryption := sa.Proposals[0].Transforms[0]
	if len(encryption.Attributes) != 1 || encryption.Attributes[0].Val != 256 {
		t.Fatalf("IKE AES transform = %+v", encryption)
	}
	if s.encKeyLen != 32 || s.integKeyLen != 64 || crypto.PRFOutputSize(s.prf) != 64 {
		t.Fatalf("IKE key sizes = enc:%d integ:%d prf:%d", s.encKeyLen, s.integKeyLen, crypto.PRFOutputSize(s.prf))
	}
}

func TestBuildIKEProposals(t *testing.T) {
	props, _, _, err := buildIKEProposals(&Config{
		IKEProposals: []string{"aes128-sha1-prfsha1-modp2048"},
	}, nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(props) != 1 {
		t.Fatalf("proposals = %d, want 1", len(props))
	}
	p := props[0]
	if p.ProtocolID != ikev2.ProtoIKE {
		t.Errorf("protocol = %d, want IKE", p.ProtocolID)
	}
	if len(p.Transforms) != 4 {
		t.Errorf("transforms = %d, want 4", len(p.Transforms))
	}
}

func TestBuildIKESAInitPacket(t *testing.T) {
	s := newInitSession(t)
	raw, err := s.buildIKESAInitPacket()
	if err != nil {
		t.Fatalf("buildIKESAInitPacket: %v", err)
	}
	pkt, err := ikev2.DecodePacket(raw)
	if err != nil {
		t.Fatal(err)
	}
	if pkt.Header.ExchangeType != ikev2.IKE_SA_INIT {
		t.Errorf("exchange = %d, want IKE_SA_INIT", pkt.Header.ExchangeType)
	}
	if pkt.Header.Flags != ikev2.FlagInitiator {
		t.Errorf("flags = %x, want 08 (initiator)", pkt.Header.Flags)
	}
	if pkt.Header.MessageID != 0 {
		t.Errorf("message id = %d, want 0", pkt.Header.MessageID)
	}
	if s.SPIi == ([8]byte{}) {
		t.Error("initiator SPI not generated")
	}
	if len(s.Ni) != 32 {
		t.Errorf("Ni length = %d, want 32", len(s.Ni))
	}
	if len(pkt.Payloads) != 4 {
		t.Fatalf("payloads = %d, want 4 (SA/KE/Ni/FRAG)", len(pkt.Payloads))
	}

	// Encode and round-trip through the ikev2 decoder.
	encoded, err := pkt.Encode()
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	dec, err := ikev2.DecodePacket(encoded)
	if err != nil {
		t.Fatalf("DecodePacket: %v", err)
	}
	if dec.Header.ExchangeType != ikev2.IKE_SA_INIT {
		t.Errorf("decoded exchange = %d", dec.Header.ExchangeType)
	}
	if len(dec.Payloads) != 4 {
		t.Fatalf("decoded payloads = %d, want 4", len(dec.Payloads))
	}

	// SA payload.
	sa, ok := dec.Payloads[0].(*ikev2.EncryptedPayloadSA)
	if !ok {
		t.Fatalf("payload 0 type = %T", dec.Payloads[0])
	}
	if len(sa.Proposals) != 1 || len(sa.Proposals[0].Transforms) != 4 {
		t.Errorf("decoded SA proposals/transforms = %d/%d", len(sa.Proposals), len(sa.Proposals[0].Transforms))
	}

	// KE payload.
	ke, ok := dec.Payloads[1].(*ikev2.EncryptedPayloadKE)
	if !ok {
		t.Fatalf("payload 1 type = %T", dec.Payloads[1])
	}
	if ke.Type() != ikev2.PayloadKE {
		t.Errorf("KE type = %d, want %d", ke.Type(), ikev2.PayloadKE)
	}
	if ke.DHGroup != 14 {
		t.Errorf("KE DH group = %d, want 14", ke.DHGroup)
	}
	// MODP-2048 public key = 256 bytes.
	if len(ke.KEData) != 256 {
		t.Errorf("KE key length = %d, want 256", len(ke.KEData))
	}

	// Nonce payload.
	ni, ok := dec.Payloads[2].(*ikev2.EncryptedPayloadNonce)
	if !ok {
		t.Fatalf("payload 2 type = %T", dec.Payloads[2])
	}
	if ni.Type() != ikev2.PayloadNi || len(ni.NonceData) != 32 {
		t.Errorf("Nonce type/length = %d/%d", ni.Type(), len(ni.NonceData))
	}
}

func TestBuildIKESAInitPacketCreatesDH(t *testing.T) {
	s := &Session{cfg: &Config{}, nonceLen: 32}
	if _, err := s.buildIKESAInitPacket(); err != nil {
		t.Fatal(err)
	}
	if s.dh == nil || s.dhGroup == 0 {
		t.Fatal("buildIKESAInitPacket did not create DH state")
	}
}
