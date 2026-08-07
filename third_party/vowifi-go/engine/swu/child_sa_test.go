package swu

import (
	"bytes"
	"encoding/binary"
	"net"
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
		if transform.TransformType == ikev2.TypeDHGroup {
			t.Fatalf("proposal contains invalid zero DH transform: %+v", transform)
		}
		if transform.TransformType == ikev2.TypeIntegrity {
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
	want := enginecrypto.PrfPlus(s.prf, s.ikeKeys.SK_d, seed, 2*directionLen)
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
	inbound, err := ipsec.Encapsulate(inner, nil, s.espInboundSA)
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
