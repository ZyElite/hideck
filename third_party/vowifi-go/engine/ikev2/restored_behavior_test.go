package ikev2

import (
	"bytes"
	"encoding/hex"
	"net"
	"testing"
)

func TestOriginalHeaderWireFormat(t *testing.T) {
	header := &IKEHeader{
		SPIi: 0x0102030405060708, SPIr: 0x1112131415161718,
		NextPayload: SA, Version: 0x20, ExchangeType: IKE_SA_INIT,
		Flags: FlagInitiator, MessageID: 7, Length: 28,
	}
	want, _ := hex.DecodeString("0102030405060708111213141516171821202208000000070000001c")
	if got := header.Encode(); !bytes.Equal(got, want) {
		t.Fatalf("header = %x, want %x", got, want)
	}
	decoded, err := DecodeHeader(want)
	if err != nil || *decoded != *header {
		t.Fatalf("DecodeHeader = %#v, %v", decoded, err)
	}
}

func TestOriginalPayloadBodyWireFormats(t *testing.T) {
	tests := []struct {
		name    string
		payload Payload
		wantHex string
	}{
		{"KE", &EncryptedPayloadKE{DHGroup: MODP_2048_bit, KEData: []byte{0xaa, 0xbb}}, "000e0000aabb"},
		{"Nonce", &EncryptedPayloadNonce{NonceData: []byte{1, 2, 3}}, "010203"},
		{"ID", &EncryptedPayloadID{IDType: ID_FQDN, IDData: []byte("x"), IsInitiator: true}, "0200000078"},
		{"AUTH", &EncryptedPayloadAuth{AuthMethod: AuthMethodSharedKey, AuthData: []byte{4, 5}}, "020000000405"},
		{"EAP", &EncryptedPayloadEAP{EAPMessage: []byte{2, 9, 0, 4}}, "02090004"},
		{"Notify", &EncryptedPayloadNotify{ProtocolID: ProtoESP, SPI: []byte{1, 2, 3, 4}, NotifyType: REKEY_SA}, "0304400901020304"},
		{"Delete", &EncryptedPayloadDelete{ProtocolID: ProtoESP, SPISize: 4, NumSPIs: 1, SPIs: []byte{1, 2, 3, 4}}, "0304000101020304"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			want, err := hex.DecodeString(test.wantHex)
			if err != nil {
				t.Fatal(err)
			}
			got, err := test.payload.Encode()
			if err != nil {
				t.Fatalf("Encode: %v", err)
			}
			if !bytes.Equal(got, want) {
				t.Fatalf("body = %x, want %x", got, want)
			}
		})
	}
}

func TestPacketChainsPayloadHeadersOutsideBodies(t *testing.T) {
	packet := &IKEPacket{
		Header: &IKEHeader{SPIi: 1, Version: 0x20, ExchangeType: IKE_SA_INIT, Flags: FlagInitiator},
		Payloads: []Payload{
			&EncryptedPayloadNonce{NonceData: []byte{1, 2}},
			&EncryptedPayloadNotify{NotifyType: INITIAL_CONTACT},
		},
	}
	raw, err := packet.Encode()
	if err != nil {
		t.Fatal(err)
	}
	wantPayloads, _ := hex.DecodeString("2900000601020000000800004000")
	if !bytes.Equal(raw[IKE_HEADER_LEN:], wantPayloads) {
		t.Fatalf("payload chain = %x, want %x", raw[IKE_HEADER_LEN:], wantPayloads)
	}
	decoded, err := DecodePacket(raw)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := decoded.Payloads[0].(*EncryptedPayloadNonce); !ok {
		t.Fatalf("payload[0] = %T", decoded.Payloads[0])
	}
	if _, ok := decoded.Payloads[1].(*EncryptedPayloadNotify); !ok {
		t.Fatalf("payload[1] = %T", decoded.Payloads[1])
	}
}

func TestOriginalMultiProposalSets(t *testing.T) {
	ikeProposals := CreateMultiProposalIKE([]byte("12345678"))
	if len(ikeProposals) != 4 {
		t.Fatalf("IKE proposal count = %d", len(ikeProposals))
	}
	if got := len(ikeProposals[0].Transforms); got != 4 {
		t.Fatalf("strict IKE transforms = %d", got)
	}
	if got := len(ikeProposals[1].Transforms); got != 10 {
		t.Fatalf("compatible IKE transforms = %d", got)
	}
	espProposals := CreateMultiProposalESP([]byte{1, 2, 3, 4})
	if len(espProposals) != 4 || len(espProposals[0].Transforms) != 2 || len(espProposals[2].Transforms) != 3 {
		t.Fatalf("ESP proposals = %#v", espProposals)
	}
	encoded, err := (&EncryptedPayloadSA{Proposals: ikeProposals}).Encode()
	if err != nil {
		t.Fatal(err)
	}
	if encoded[0] != 2 || !ikeProposals[3].LastProposal {
		t.Fatalf("proposal chaining not restored: first=%d last=%t", encoded[0], ikeProposals[3].LastProposal)
	}
}

func TestProposalMatcherUsesConfiguredPriority(t *testing.T) {
	proposal := NewProposal(1, ProtoIKE, nil)
	proposal.AddTransformWithKeyLen(TransformTypeEncr, ENCR_AES_CBC, 128)
	proposal.AddTransformWithKeyLen(TransformTypeEncr, ENCR_AES_GCM_16, 256)
	proposal.AddTransform(TransformTypePRF, PRF_HMAC_SHA1)
	proposal.AddTransform(TransformTypePRF, PRF_HMAC_SHA2_256)
	proposal.AddTransform(TransformTypeDH, MODP_2048_bit)
	matched, err := DefaultProposalMatcher().SelectBestProposal(&EncryptedPayloadSA{Proposals: []*Proposal{proposal}})
	if err != nil {
		t.Fatal(err)
	}
	if matched == nil || matched.Encr != ENCR_AES_GCM_16 || matched.EncrKeyLen != 256 ||
		matched.PRF != PRF_HMAC_SHA2_256 || matched.DH != MODP_2048_bit {
		t.Fatalf("matched = %#v", matched)
	}
}

func TestCPConfigPreservesAddressFamiliesAndOrder(t *testing.T) {
	payload := &EncryptedPayloadCP{CFGType: CFG_REPLY, Attributes: []*CPAttribute{
		{Type: INTERNAL_IP4_ADDRESS, Value: net.IPv4(10, 0, 0, 2).To4()},
		{Type: INTERNAL_IP4_DNS, Value: net.IPv4(8, 8, 8, 8).To4()},
		{Type: INTERNAL_IP6_ADDRESS, Value: append(net.ParseIP("2001:db8::2").To16(), 64)},
		{Type: P_CSCF_IP6_ADDRESS, Value: net.ParseIP("2001:db8::10").To16()},
	}}
	config := ParseCPConfig(payload)
	if !config.HasIPv4() || !config.HasIPv6() || config.IPv6Prefix != 64 {
		t.Fatalf("config = %#v", config)
	}
	if !config.IPv4Addresses[0].Equal(net.IPv4(10, 0, 0, 2)) ||
		!config.IPv6PCSCF[0].Equal(net.ParseIP("2001:db8::10")) {
		t.Fatalf("parsed addresses = %#v", config)
	}
}

func TestNATDetectionOriginalKnownVector(t *testing.T) {
	want, _ := hex.DecodeString("d798d986143f878f70765e0e869c80bbc375f701")
	got := CalculateNATDetectionHash(
		uint64(0x0102030405060708), uint64(0x1112131415161718),
		[]byte{192, 0, 2, 1}, uint16(500),
	)
	if !bytes.Equal(got, want) {
		t.Fatalf("NAT-D = %x, want %x", got, want)
	}
}

func TestMalformedPayloadLengthsAreRejected(t *testing.T) {
	header := (&IKEHeader{NextPayload: KE, Version: 0x20, ExchangeType: IKE_SA_INIT, Length: 32}).Encode()
	badPacket := append(header, []byte{0, 0, 0, 3}...)
	if _, err := DecodePacket(badPacket); err == nil {
		t.Fatal("accepted a payload length below the generic header size")
	}
	if _, err := DecodePayloadDelete([]byte{3, 4, 0, 1, 1, 2}); err == nil {
		t.Fatal("accepted truncated Delete SPI data")
	}
	if _, err := DecodePayloadTS([]byte{1, 0, 0, 0, 7, 0, 0, 40}, true); err == nil {
		t.Fatal("accepted truncated traffic selector")
	}
}
