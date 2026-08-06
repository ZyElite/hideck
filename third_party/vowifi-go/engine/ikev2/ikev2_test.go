package ikev2

import (
	"bytes"
	"net"
	"testing"
)

func TestPacketRoundTrip(t *testing.T) {
	pkt := &IKEPacket{
		InitiatorSPI: [8]byte{1, 2, 3, 4, 5, 6, 7, 8},
		ResponderSPI: [8]byte{9, 10, 11, 12, 13, 14, 15, 16},
		Version:      0x20,
		ExchangeType: ExchangeIKEInit,
		Flags:        0x08, // Initiator
		MessageID:    1,
		Payloads: []Payload{
			&EncryptedPayloadSA{Proposals: CreateMultiProposalIKE(12, 2, 2, 14)}, // AES-CBC/HMAC-SHA1/HMAC-SHA1/MODP-2048
			&EncryptedPayloadKE{DHGroupNum: 14, KeyData: []byte{0xde, 0xad, 0xbe, 0xef}},
			&EncryptedPayloadNonce{Data: []byte("nonce-data-1234")},
			&EncryptedPayloadNotify{ProtocolID: ProtoIKE, NotifyType: 16388, NotifyData: []byte{1, 2, 3, 4, 5, 6, 7, 8}},
		},
	}

	raw := pkt.Encode()
	if len(raw) < 28 {
		t.Fatalf("encoded too short: %d", len(raw))
	}

	dec, err := DecodePacket(raw)
	if err != nil {
		t.Fatalf("DecodePacket: %v", err)
	}
	if dec.ExchangeType != ExchangeIKEInit || dec.MessageID != 1 {
		t.Errorf("header mismatch: %+v", dec)
	}
	if !bytes.Equal(dec.InitiatorSPI[:], pkt.InitiatorSPI[:]) {
		t.Error("initiator SPI mismatch")
	}
	if len(dec.Payloads) != 4 {
		t.Fatalf("got %d payloads, want 4", len(dec.Payloads))
	}

	// The first payload must be the SA (decoded structurally).
	sa, ok := dec.Payloads[0].(*EncryptedPayloadSA)
	if !ok {
		t.Fatalf("payload 0 type = %T", dec.Payloads[0])
	}
	if len(sa.Proposals) != 1 || len(sa.Proposals[0].Transforms) != 4 {
		t.Errorf("SA proposals/transforms = %d/%d", len(sa.Proposals), len(sa.Proposals[0].Transforms))
	}

	// The KE payload keeps its DH group header and key bytes
	// (body = DH group 2B | reserved 2B | key data).
	ke, ok := dec.Payloads[1].(*RawPayload)
	if !ok {
		t.Fatalf("payload 1 type = %T", dec.Payloads[1])
	}
	if ke.Type() != PayloadKE || !bytes.Equal(ke.Data[4:8], []byte{0xde, 0xad, 0xbe, 0xef}) {
		t.Errorf("KE payload = type %d data %x", ke.Type(), ke.Data)
	}
}

func TestProposalEncodeDecode(t *testing.T) {
	p := &Proposal{ProposalNum: 1, ProtocolID: ProtoESP}
	p.AddTransformWithKeyLen(TypeEncryption, 12, 128) // AES-CBC with 128-bit key
	p.AddTransform(TypeIntegrity, 2)                  // HMAC-SHA1-96
	p.AddTransform(TypeDHGroup, 14)

	enc := p.Encode(nil)
	dec, n, err := DecodeProposal(enc)
	if err != nil {
		t.Fatalf("DecodeProposal: %v", err)
	}
	if n != len(enc) {
		t.Errorf("consumed %d of %d", n, len(enc))
	}
	if len(dec.Transforms) != 3 {
		t.Fatalf("transforms = %d, want 3", len(dec.Transforms))
	}
	if dec.Transforms[0].TransformID != 12 || len(dec.Transforms[0].Attributes) != 1 {
		t.Errorf("transform 0 = %+v", dec.Transforms[0])
	}
	if dec.Transforms[0].Attributes[0].Value != 128 {
		t.Errorf("key length attr = %d, want 128", dec.Transforms[0].Attributes[0].Value)
	}
}

func TestTrafficSelector(t *testing.T) {
	ts := NewTrafficSelectorIPV4(net.ParseIP("10.0.0.1"), 17, 0, 65535)
	if ts.Type != TSIPv4Range || ts.StartPort != 0 || ts.EndPort != 65535 {
		t.Errorf("ts = %+v", ts)
	}
	if !bytes.Equal(ts.StartAddr, []byte{10, 0, 0, 1}) {
		t.Errorf("start addr = %x", ts.StartAddr)
	}

	ts6 := NewTrafficSelectorIPV6(net.ParseIP("2001:db8::1"), 0, 0, 65535)
	if ts6.Type != TSIPv6Range || len(ts6.StartAddr) != 16 {
		t.Errorf("ts6 = %+v", ts6)
	}
}

func TestNotifyTypeToString(t *testing.T) {
	cases := []struct {
		t    uint16
		want string
	}{
		{1, "INVALID_SA_PROPOSAL"},
		{16388, "NAT_DETECTION_SOURCE_IP"},
		{16393, "REKEY_SA"},
		{999, "999"},
	}
	for _, c := range cases {
		if got := NotifyTypeToString(c.t); got != c.want {
			t.Errorf("NotifyTypeToString(%d) = %q, want %q", c.t, got, c.want)
		}
	}
}

func TestNATDetectionHash(t *testing.T) {
	key := []byte("prf-key")
	spiI := [8]byte{1, 1, 1, 1, 1, 1, 1, 1}
	spiR := [8]byte{2, 2, 2, 2, 2, 2, 2, 2}
	ip := net.ParseIP("192.168.1.100")
	h1 := CalculateNATDetectionHash(key, spiI, spiR, ip, 500)
	h2 := CalculateNATDetectionHash(key, spiI, spiR, ip, 500)
	if !bytes.Equal(h1, h2) {
		t.Error("NAT hash not deterministic")
	}
	if len(h1) != 20 { // SHA1
		t.Errorf("hash length = %d, want 20", len(h1))
	}
}

func TestCPConfig(t *testing.T) {
	cp := &EncryptedPayloadCP{
		ConfigType: 1, // CFG_REQUEST
		Attrs: []*CPAttribute{
			{Type: 1, Value: []byte{10, 0, 0, 2}}, // INTERNAL_IP4_ADDRESS
			{Type: 3, Value: []byte{10, 0, 0, 1}}, // INTERNAL_IP4_DNS
		},
	}
	raw := cp.Encode(nil)
	dec, err := DecodePayloadCP(raw[4:])
	if err != nil {
		t.Fatalf("DecodePayloadCP: %v", err)
	}
	cfg := ParseCPConfig(dec)
	if !cfg.HasIPv4() {
		t.Error("HasIPv4 = false, want true")
	}
	if cfg.HasIPv6() {
		t.Error("HasIPv6 = true, want false")
	}
	if got := cfg.Attrs[3]; !bytes.Equal(got, []byte{10, 0, 0, 1}) {
		t.Errorf("DNS attr = %x", got)
	}
}
