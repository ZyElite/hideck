package swu

import (
	"bytes"
	"encoding/binary"
	"net"
	"strings"
	"testing"

	enginecrypto "github.com/iniwex5/vowifi-go/engine/crypto"
	"github.com/iniwex5/vowifi-go/engine/ikev2"
	"github.com/iniwex5/vowifi-go/engine/ipsec"
)

func TestBuildESPProposalsCarriesInboundSPI(t *testing.T) {
	const spi = uint32(0x10203040)
	proposal := buildESPProposals(enginecrypto.EncrAESGCM16, 0, spi)[0]
	if got := binary.BigEndian.Uint32(proposal.SPI); got != spi {
		t.Fatalf("proposal SPI = %08x, want %08x", got, spi)
	}
	for _, transform := range proposal.Transforms {
		if transform.Type == ikev2.TransformTypeDH {
			t.Fatalf("proposal contains invalid zero DH transform: %+v", transform)
		}
		if transform.Type == ikev2.TransformTypeInteg {
			t.Fatalf("AEAD proposal contains separate integrity transform: %+v", transform)
		}
	}
}

func TestDeriveChildSAKeysUsesFreshNoncesAndBothDirections(t *testing.T) {
	s := NewSession(&Config{})
	s.ikeKeys = &IKEKeys{SK_d: bytes.Repeat([]byte{0x31}, 20)}
	s.childNi = bytes.Repeat([]byte{0x41}, 32)
	s.childNr = bytes.Repeat([]byte{0x51}, 32)

	keys, err := s.deriveChildSAKeys()
	if err != nil {
		t.Fatalf("deriveChildSAKeys: %v", err)
	}
	directionLen := s.espEncKeyLen + s.espIntegKeyLen
	seed := append(append([]byte{}, s.childNi...), s.childNr...)
	want, err := enginecrypto.PrfPlus(s.prf, s.ikeKeys.SK_d, seed, 2*directionLen)
	if err != nil {
		t.Fatalf("PrfPlus: %v", err)
	}
	if !bytes.Equal(keys.initiator.enc, want[:s.espEncKeyLen]) ||
		!bytes.Equal(keys.responder.enc, want[directionLen:directionLen+s.espEncKeyLen]) {
		t.Fatal("directional CHILD_SA encryption keys do not match RFC 7296 KEYMAT order")
	}
	if bytes.Equal(keys.initiator.enc, keys.responder.enc) {
		t.Fatal("initiator and responder encryption keys must be distinct")
	}
}

func TestDataPlaneUsesIndependentInboundAndOutboundSPI(t *testing.T) {
	s := NewSession(&Config{})
	s.ikeKeys = &IKEKeys{SK_d: bytes.Repeat([]byte{0x61}, 20)}
	s.childNi = bytes.Repeat([]byte{0x71}, 32)
	s.childNr = bytes.Repeat([]byte{0x81}, 32)
	s.espLocalSPI = 0x11223344
	s.espRemoteSPI = 0x55667788
	s.innerIP = net.IPv4(10, 0, 0, 2)
	if err := s.setupDataPlane(); err != nil {
		t.Fatalf("setupDataPlane: %v", err)
	}

	inner := append([]byte{0x45}, make([]byte, 31)...)
	outbound, err := s.encapsulateInnerPacket(inner)
	if err != nil {
		t.Fatalf("encapsulateInnerPacket: %v", err)
	}
	if got := binary.BigEndian.Uint32(outbound[:4]); got != s.espRemoteSPI {
		t.Fatalf("outbound SPI = %08x, want remote %08x", got, s.espRemoteSPI)
	}
	inbound, err := ipsec.Encapsulate(inner, s.espInboundSA)
	if err != nil {
		t.Fatalf("build inbound packet: %v", err)
	}
	decoded, err := s.decapsulateOuterESP(inbound)
	if err != nil {
		t.Fatalf("decapsulateOuterESP: %v", err)
	}
	if !bytes.Equal(decoded, inner) {
		t.Fatal("inbound ESP plaintext mismatch")
	}
}

func TestDataPlaneEncapsulationLeaseReleasesPooledBuffer(t *testing.T) {
	s := NewSession(&Config{})
	s.ikeKeys = &IKEKeys{SK_d: bytes.Repeat([]byte{0x61}, 20)}
	s.childNi = bytes.Repeat([]byte{0x71}, 32)
	s.childNr = bytes.Repeat([]byte{0x81}, 32)
	s.espLocalSPI = 0x11223344
	s.espRemoteSPI = 0x55667788
	s.innerIP = net.IPv4(10, 0, 0, 2)
	if err := s.setupDataPlane(); err != nil {
		t.Fatalf("setupDataPlane: %v", err)
	}

	inner := append([]byte{0x45}, make([]byte, 31)...)
	lease, err := s.encapsulateInnerPacketLease(inner)
	if err != nil {
		t.Fatalf("encapsulateInnerPacketLease: %v", err)
	}
	if len(lease.data) == 0 || cap(lease.buffer.Bytes()) < len(lease.data) {
		t.Fatalf("invalid lease: data length %d, capacity %d", len(lease.data), cap(lease.buffer.Bytes()))
	}
	decoded, err := ipsec.Decapsulate(lease.data, s.espOutboundSA)
	if err != nil {
		t.Fatalf("decapsulate leased ESP packet: %v", err)
	}
	if !bytes.Equal(decoded, inner) {
		t.Fatal("leased ESP plaintext mismatch")
	}
	lease.Release()
	if lease.data != nil || lease.buffer.Bytes() != nil {
		t.Fatal("packet lease retained data after Release")
	}
}

func TestValidateChildSAResponseAcceptsExactProposalAndNarrowing(t *testing.T) {
	innerIP := net.IPv4(10, 0, 0, 2)
	offer := testChildSAOffer(innerIP)
	payloads := validChildSAResponsePayloads(innerIP)
	selection, err := validateChildSAResponse(payloads, offer)
	if err != nil {
		t.Fatalf("validateChildSAResponse: %v", err)
	}
	if selection.remoteSPI != 0x55667788 || selection.encryption != enginecrypto.EncrAESCBC || selection.integrity != 2 ||
		len(selection.nonce) != 32 {
		t.Fatalf("selection = %+v", selection)
	}
	selectedTSr := payloads[3].(*ikev2.EncryptedPayloadTS)
	selectedTSr.TrafficSelectors[0].StartAddr[3] = 99
	if selection.tsr.TrafficSelectors[0].StartAddr[3] != 1 {
		t.Fatal("selection retained mutable response selector storage")
	}
}

func TestValidateChildSAResponseRejectsInvalidSelections(t *testing.T) {
	innerIP := net.IPv4(10, 0, 0, 2)
	tests := map[string]struct {
		mutate func([]ikev2.Payload)
		want   string
	}{
		"protocol": {
			mutate: func(payloads []ikev2.Payload) { childProposal(payloads).ProtocolID = ikev2.ProtoIKE },
			want:   "invalid ESP proposal",
		},
		"algorithm": {
			mutate: func(payloads []ikev2.Payload) {
				childProposal(payloads).Transforms[0].ID = ikev2.AlgorithmType(enginecrypto.EncrAESGCM16)
			},
			want: "encryption selection",
		},
		"key length": {
			mutate: func(payloads []ikev2.Payload) { childProposal(payloads).Transforms[0].Attributes[0].Val = 256 },
			want:   "128-bit KEY_LENGTH",
		},
		"TSi excludes local": {
			mutate: func(payloads []ikev2.Payload) {
				payloads[2].(*ikev2.EncryptedPayloadTS).TrafficSelectors[0] = ikev2.NewTrafficSelectorIPV4(net.IPv4(10, 0, 0, 3), 0, 0, 0xffff)
			},
			want: "TSi is not a legal narrowing",
		},
		"TSr outside offer": {
			mutate: func(payloads []ikev2.Payload) {
				payloads[3].(*ikev2.EncryptedPayloadTS).TrafficSelectors[0] = ikev2.NewTrafficSelectorIPV4(net.IPv4(198, 51, 100, 1), 0, 0, 0xffff)
			},
			want: "TSr is not a legal narrowing",
		},
		"nonce missing": {
			mutate: func(payloads []ikev2.Payload) { payloads[1] = &ikev2.EncryptedPayloadNonce{} },
			want:   "missing nonce",
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			payloads := validChildSAResponsePayloads(innerIP)
			test.mutate(payloads)
			_, err := validateChildSAResponse(payloads, testChildSAOffer(innerIP))
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error = %v, want %q", err, test.want)
			}
		})
	}
}

func TestNegotiatedSelectorsConstrainDataPlane(t *testing.T) {
	innerIP := net.IPv4(10, 0, 0, 2)
	selection, err := validateChildSAResponse(validChildSAResponsePayloads(innerIP), testChildSAOffer(innerIP))
	if err != nil {
		t.Fatalf("validateChildSAResponse: %v", err)
	}
	session := NewSession(&Config{})
	session.espOutboundSA = ipsec.NewSecurityAssociation(0x55667788, enginecrypto.EncrAESGCM16, bytes.Repeat([]byte{0x33}, 20), 0)
	session.childTSi, session.childTSr = selection.tsi, selection.tsr
	allowed := testIPv4Flow(innerIP, net.IPv4(192, 0, 2, 1))
	if _, err := session.encapsulateInnerPacket(allowed); err != nil {
		t.Fatalf("allowed packet: %v", err)
	}
	denied := testIPv4Flow(innerIP, net.IPv4(192, 0, 2, 2))
	if _, err := session.encapsulateInnerPacket(denied); err == nil || !strings.Contains(err.Error(), "outside negotiated") {
		t.Fatalf("denied packet error = %v", err)
	}
}

func testChildSAOffer(innerIP net.IP) childSAOffer {
	return childSAOffer{
		encryption:        enginecrypto.EncrAESCBC,
		encryptionKeyBits: 128,
		integrity:         2,
		tsi: &ikev2.EncryptedPayloadTS{IsInitiator: true, TrafficSelectors: []*ikev2.TrafficSelector{
			ikev2.NewTrafficSelectorIPV4(innerIP, 0, 0, 0xffff),
		}},
		tsr: &ikev2.EncryptedPayloadTS{TrafficSelectors: []*ikev2.TrafficSelector{
			ikev2.NewTrafficSelectorIPV4Range(net.IPv4(192, 0, 2, 0), net.IPv4(192, 0, 2, 255), 0, 0, 0xffff),
		}},
		localIPs: []net.IP{innerIP}, requireSA: true, requireNonce: true,
	}
}

func validChildSAResponsePayloads(innerIP net.IP) []ikev2.Payload {
	proposal := &ikev2.Proposal{
		ProposalNum: 1, ProtocolID: ikev2.ProtoESP, SPISize: 4, NumTransforms: 3,
		SPI: []byte{0x55, 0x66, 0x77, 0x88},
		Transforms: []*ikev2.Transform{
			{Type: ikev2.TransformTypeEncr, ID: ikev2.AlgorithmType(enginecrypto.EncrAESCBC), Attributes: []*ikev2.TransformAttribute{{Type: 14, Val: 128}}},
			{Type: ikev2.TransformTypeInteg, ID: 2},
			{Type: ikev2.TransformTypeESN, ID: 0},
		},
	}
	return []ikev2.Payload{
		&ikev2.EncryptedPayloadSA{Proposals: []*ikev2.Proposal{proposal}},
		&ikev2.EncryptedPayloadNonce{NonceData: bytes.Repeat([]byte{0x44}, 32)},
		&ikev2.EncryptedPayloadTS{IsInitiator: true, TrafficSelectors: []*ikev2.TrafficSelector{
			ikev2.NewTrafficSelectorIPV4(innerIP, 0, 0, 0xffff),
		}},
		&ikev2.EncryptedPayloadTS{TrafficSelectors: []*ikev2.TrafficSelector{
			ikev2.NewTrafficSelectorIPV4(net.IPv4(192, 0, 2, 1), 0, 0, 0xffff),
		}},
	}
}

func childProposal(payloads []ikev2.Payload) *ikev2.Proposal {
	return payloads[0].(*ikev2.EncryptedPayloadSA).Proposals[0]
}

func testIPv4Flow(source, destination net.IP) []byte {
	packet := make([]byte, 20)
	packet[0], packet[9] = 0x45, 1
	copy(packet[12:16], source.To4())
	copy(packet[16:20], destination.To4())
	return packet
}
