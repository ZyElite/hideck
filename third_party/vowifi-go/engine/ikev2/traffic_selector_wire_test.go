package ikev2

import (
	"bytes"
	"net"
	"testing"
)

func TestTrafficSelectorIPv4WireFormat(t *testing.T) {
	selector := NewTrafficSelectorIPV4(net.IPv4(10, 0, 0, 1), 17, 5060, 5060)
	got := selector.Encode(nil)
	want := []byte{
		0x07, 0x11, 0x00, 0x10,
		0x13, 0xc4, 0x13, 0xc4,
		0x0a, 0x00, 0x00, 0x01,
		0x0a, 0x00, 0x00, 0x01,
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("selector wire = %x, want %x", got, want)
	}
}

func TestTrafficSelectorPayloadTypesStayDistinct(t *testing.T) {
	tsi := &EncryptedPayloadTS{PayloadType: PayloadTSi, Selectors: []*TrafficSelector{
		NewTrafficSelectorIPV4(net.IPv4zero, 0, 0, 0xffff),
	}}
	tsr := &EncryptedPayloadTS{PayloadType: PayloadTSr, Selectors: []*TrafficSelector{
		NewTrafficSelectorIPV4(net.IPv4zero, 0, 0, 0xffff),
	}}
	raw := EncodePayloadChain([]Payload{tsi, tsr})
	if raw[0] != PayloadTSr {
		t.Fatalf("TSi next payload = %d, want TSr %d", raw[0], PayloadTSr)
	}
	decoded, err := DecodePayloadChainWithFirst(PayloadTSi, raw)
	if err != nil {
		t.Fatalf("decode selectors: %v", err)
	}
	if len(decoded) != 2 || decoded[0].Type() != PayloadTSi || decoded[1].Type() != PayloadTSr {
		t.Fatalf("decoded selector payload types = %#v", decoded)
	}
}

func TestAESProposalCarries128BitKeyLength(t *testing.T) {
	proposal := CreateMultiProposalIKE(encrAESCBC, 2, 2, 14)[0]
	raw := proposal.Encode(nil)
	wantEncryptionTransform := []byte{
		0x03, 0x00, 0x00, 0x0c,
		0x01, 0x00, 0x00, 0x0c,
		0x80, 0x0e, 0x00, 0x80,
	}
	if !bytes.Contains(raw, wantEncryptionTransform) {
		t.Fatalf("proposal %x does not contain AES KEY_LENGTH transform %x", raw, wantEncryptionTransform)
	}
}

func TestAESProposalCarriesExplicit256BitKeyLength(t *testing.T) {
	proposal := CreateIKEProposals(IKEProposalAlgorithms{
		Encryption: encrAESCBC, EncryptionKeyBits: 256,
		PRF: 7, Integrity: 14, DH: 14,
	})[0]
	raw := proposal.Encode(nil)
	wantEncryptionTransform := []byte{
		0x03, 0x00, 0x00, 0x0c,
		0x01, 0x00, 0x00, 0x0c,
		0x80, 0x0e, 0x01, 0x00,
	}
	if !bytes.Contains(raw, wantEncryptionTransform) {
		t.Fatalf("proposal %x does not contain AES-256 KEY_LENGTH transform %x", raw, wantEncryptionTransform)
	}
}
