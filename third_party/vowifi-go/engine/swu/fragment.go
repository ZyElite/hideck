package swu

import (
	"fmt"

	"github.com/iniwex5/vowifi-go/engine/ikev2"
)

const (
	defaultFragmentMTU       = 1280
	ikeFragmentFixedOverhead = 20 + 8 + ikev2.IKE_HEADER_LEN + 4 + 4
	maxFragments             = 255
)

type fragmentMessageSpec struct {
	capacity     int
	messageID    uint32
	exchangeType ikev2.ExchangeType
	flags        uint8
	firstPayload ikev2.PayloadType
}

// shouldFragment restores the negotiated RFC 7383 size decision.
func (s *Session) shouldFragment(payloads []ikev2.Payload) bool {
	return s.shouldFragmentWithFlags(payloads, s.localIKEFlags(false))
}

func (s *Session) shouldFragmentWithFlags(payloads []ikev2.Payload, flags uint8) bool {
	s.mu.RLock()
	supported := s.fragmentationSupported
	mtu := s.ikeFragmentMTU
	s.mu.RUnlock()
	if !supported {
		return false
	}
	inner, err := ikev2.EncodePayloadChainChecked(payloads)
	if err != nil {
		return false
	}
	cipher, _, integrity, err := s.fragmentCipherMaterial(flags)
	if err != nil {
		return false
	}
	if mtu == 0 {
		mtu = defaultFragmentMTU
	}
	authSize := aesGCMTagLength
	if !s.aead {
		authSize = integrity.OutputSize()
	}
	padded := padIKEPlaintext(inner, cipher.BlockSize())
	wireSize := 20 + 8 + ikev2.IKE_HEADER_LEN + 4 + cipher.IVSize() + len(padded) + authSize
	return wireSize > int(mtu)
}

// fragmentMessage splits the plaintext payload chain and protects every
// fragment independently while retaining one IKE Message ID.
func (s *Session) fragmentMessage(
	payloads []ikev2.Payload,
	exchangeType ikev2.ExchangeType,
) ([][]byte, error) {
	inner, err := ikev2.EncodePayloadChainChecked(payloads)
	if err != nil {
		return nil, err
	}
	flags := s.localIKEFlags(false)
	capacity, err := s.fragmentPlaintextCapacity(flags)
	if err != nil {
		return nil, err
	}
	fragmentCount := (len(inner) + capacity - 1) / capacity
	if fragmentCount <= 1 {
		return nil, nil
	}
	if fragmentCount > maxFragments {
		return nil, fmt.Errorf("IKE fragment count %d exceeds maximum %d", fragmentCount, maxFragments)
	}
	firstPayload := ikev2.NoNextPayload
	if len(payloads) > 0 {
		firstPayload = payloads[0].Type()
	}
	return s.fragmentPlaintext(inner, fragmentMessageSpec{
		capacity: capacity, messageID: s.nextMessageID(), exchangeType: exchangeType,
		flags: flags, firstPayload: firstPayload,
	})
}

func (s *Session) fragmentResponse(
	payloads []ikev2.Payload,
	exchangeType ikev2.ExchangeType,
	messageID uint32,
) ([][]byte, error) {
	inner, err := ikev2.EncodePayloadChainChecked(payloads)
	if err != nil {
		return nil, err
	}
	flags := s.localIKEFlags(true)
	capacity, err := s.fragmentPlaintextCapacity(flags)
	if err != nil {
		return nil, err
	}
	fragmentCount := (len(inner) + capacity - 1) / capacity
	if fragmentCount <= 1 {
		return nil, nil
	}
	if fragmentCount > maxFragments {
		return nil, fmt.Errorf("IKE fragment count %d exceeds maximum %d", fragmentCount, maxFragments)
	}
	return s.fragmentPlaintext(inner, fragmentMessageSpec{
		capacity: capacity, messageID: messageID, exchangeType: exchangeType,
		flags: flags, firstPayload: firstIKEPayloadType(payloads),
	})
}

func (s *Session) fragmentPlaintext(inner []byte, spec fragmentMessageSpec) ([][]byte, error) {
	fragmentCount := (len(inner) + spec.capacity - 1) / spec.capacity
	packets := make([][]byte, 0, fragmentCount)
	for index := 0; index < fragmentCount; index++ {
		start := index * spec.capacity
		end := min(start+spec.capacity, len(inner))
		packetSpec := fragmentPacketSpec{
			number: uint16(index + 1), total: uint16(fragmentCount),
			messageID: spec.messageID, exchangeType: spec.exchangeType,
			flags: spec.flags, firstPayload: spec.firstPayload,
		}
		packet, err := s.buildSKFPacketWithFlags(inner[start:end], packetSpec)
		if err != nil {
			return nil, fmt.Errorf("build IKE fragment %d/%d: %w", index+1, fragmentCount, err)
		}
		packets = append(packets, packet)
	}
	return packets, nil
}

func (s *Session) fragmentPlaintextCapacity(flags uint8) (int, error) {
	cipher, _, integrity, err := s.fragmentCipherMaterial(flags)
	if err != nil {
		return 0, err
	}
	s.mu.RLock()
	mtu := s.ikeFragmentMTU
	s.mu.RUnlock()
	if mtu == 0 {
		mtu = defaultFragmentMTU
	}
	authSize := aesGCMTagLength
	if !s.aead {
		authSize = integrity.OutputSize()
	}
	capacity := int(mtu) - ikeFragmentFixedOverhead - cipher.IVSize() - authSize - cipher.BlockSize()
	if capacity <= 0 {
		return 0, fmt.Errorf("IKE fragment MTU %d is too small", mtu)
	}
	return capacity, nil
}
