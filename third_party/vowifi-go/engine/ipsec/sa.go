package ipsec

import (
	"encoding/binary"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/iniwex5/vowifi-go/engine/crypto"
)

// SecurityAssociation mirrors the legacy ESP SA. The prepared cipher is
// cached because creating AES-GCM/AES-CBC state for every packet is costly.
type SecurityAssociation struct {
	SequenceNumber uint64
	ReplayWindow   uint64
	SPI            uint32
	RemoteSPI      uint32
	EncryptionAlg  crypto.Encrypter
	EncryptionKey  []byte
	IntegritySalt  []byte

	preparedCipher    crypto.PreparedCipher
	preparedCipherErr error
	IntegrityAlg      crypto.PRF
	IntegrityAlg2     crypto.IntegrityAlgorithm
	IntegrityKey      []byte
	IsAEAD            bool
	mu                sync.Mutex

	replayMu       sync.Mutex
	inboundHighest uint32
	inboundBitmap  uint64
}

const replayWindowSize = 64

// NewSecurityAssociation creates an AEAD ESP SA. The fourth argument is the
// legacy integrity-key slot; a uint32 is also accepted for compatibility with
// the earlier reconstructed RemoteSPI constructor.
func NewSecurityAssociation(spi uint32, algorithm any, key []byte, final any) *SecurityAssociation {
	enc, resolveErr := resolveESPEncrypter(algorithm, key)
	sa := &SecurityAssociation{
		SPI: spi, EncryptionAlg: enc, EncryptionKey: key, IsAEAD: true,
	}
	switch value := final.(type) {
	case []byte:
		sa.IntegrityKey = value
	case uint32:
		sa.RemoteSPI = value
	case int:
		if value < 0 || uint64(value) > uint64(^uint32(0)) {
			resolveErr = fmt.Errorf("ipsec: invalid remote SPI %d", value)
		} else {
			sa.RemoteSPI = uint32(value)
		}
	case nil:
	default:
		resolveErr = fmt.Errorf("ipsec: invalid AEAD constructor argument %T", final)
	}
	sa.prepare(resolveErr)
	return sa
}

// NewSecurityAssociationCBC creates a CBC ESP SA. remoteSPI is an additive
// compatibility argument retained from the reconstructed implementation.
func NewSecurityAssociationCBC(
	spi uint32,
	algorithm any,
	key []byte,
	integrity crypto.IntegrityAlgorithm,
	integrityKey []byte,
	remoteSPI ...uint32,
) *SecurityAssociation {
	enc, resolveErr := resolveESPEncrypter(algorithm, key)
	sa := &SecurityAssociation{
		SPI: spi, EncryptionAlg: enc, EncryptionKey: key,
		IntegrityAlg2: integrity, IntegrityKey: integrityKey,
	}
	if len(remoteSPI) > 0 {
		sa.RemoteSPI = remoteSPI[0]
	}
	sa.prepare(resolveErr)
	return sa
}

func resolveESPEncrypter(algorithm any, key []byte) (crypto.Encrypter, error) {
	if enc, ok := algorithm.(crypto.Encrypter); ok {
		return enc, nil
	}
	id, ok := algorithm.(uint16)
	if !ok {
		return nil, fmt.Errorf("ipsec: invalid encryption algorithm %T", algorithm)
	}
	keyBits := len(key) * 8
	if id == crypto.EncrAESGCM8 || id == crypto.EncrAESGCM12 || id == crypto.EncrAESGCM16 {
		keyBits -= 32
	}
	return crypto.GetEncrypterWithKeyLen(id, keyBits)
}

func (sa *SecurityAssociation) prepare(resolveErr error) {
	if resolveErr != nil {
		sa.preparedCipherErr = resolveErr
		return
	}
	sa.preparedCipher, sa.preparedCipherErr = crypto.PrepareCipher(sa.EncryptionAlg, sa.EncryptionKey)
}

// NextSequenceNumber returns the next outbound ESP sequence number.
func (sa *SecurityAssociation) NextSequenceNumber() uint32 {
	return uint32(atomic.AddUint64(&sa.SequenceNumber, 1))
}

func (sa *SecurityAssociation) reserveSequenceNumber() (uint32, error) {
	for {
		current := atomic.LoadUint64(&sa.SequenceNumber)
		if current >= uint64(^uint32(0)) {
			return 0, errSequenceExhausted
		}
		if atomic.CompareAndSwapUint64(&sa.SequenceNumber, current, current+1) {
			return uint32(current + 1), nil
		}
	}
}

func (sa *SecurityAssociation) acceptInboundSequence(sequence uint32) error {
	if sequence == 0 {
		return errInvalidSequence
	}
	sa.replayMu.Lock()
	defer sa.replayMu.Unlock()
	return sa.updateReplayWindow(sequence)
}

func (sa *SecurityAssociation) updateReplayWindow(sequence uint32) error {
	if sa.inboundHighest == 0 {
		sa.inboundHighest, sa.inboundBitmap = sequence, 1
		return nil
	}
	if sequence > sa.inboundHighest {
		shift := sequence - sa.inboundHighest
		if shift >= replayWindowSize {
			sa.inboundBitmap = 1
		} else {
			sa.inboundBitmap = (sa.inboundBitmap << shift) | 1
		}
		sa.inboundHighest = sequence
		return nil
	}
	distance := sa.inboundHighest - sequence
	if distance >= replayWindowSize {
		return errSequenceTooOld
	}
	mask := uint64(1) << distance
	if sa.inboundBitmap&mask != 0 {
		return errSequenceReplay
	}
	sa.inboundBitmap |= mask
	return nil
}

func (sa *SecurityAssociation) cipher() (crypto.PreparedCipher, error) {
	if sa == nil {
		return nil, errInvalidSA
	}
	sa.mu.Lock()
	defer sa.mu.Unlock()
	if sa.preparedCipher == nil && sa.preparedCipherErr == nil && sa.EncryptionAlg != nil {
		sa.prepare(nil)
	}
	return sa.preparedCipher, sa.preparedCipherErr
}

func (sa *SecurityAssociation) overhead() int {
	if sa.IsAEAD {
		return 16
	}
	if sa.IntegrityAlg2 != nil {
		return sa.IntegrityAlg2.OutputSize()
	}
	return 0
}

var (
	errInvalidSA         = errors.New("ESP security association is nil")
	errNoCipher          = errors.New("ESP encryption algorithm is nil")
	errCipherUnavailable = errors.New("ESP cipher is nil")
	errSPIMismatch       = errors.New("ESP SPI 不匹配")
	errPacketTooShort    = errors.New("ESP packet too short")
	errIVTooShort        = errors.New("ESP packet too short for IV")
	errICVTooShort       = errors.New("ESP packet too short for ICV")
	errIntegrityFailed   = errors.New("ESP integrity check failed")
	errPayloadTooShort   = errors.New("decrypted payload too short")
	errBadPaddingLength  = errors.New("invalid padding length")
	errInvalidPadding    = errors.New("invalid ESP padding bytes")
	errInvalidBlockSize  = errors.New("ESP encryption block size is invalid")
	errSequenceExhausted = errors.New("ESP sequence number exhausted")
	errInvalidSequence   = errors.New("invalid ESP sequence number zero")
	errSequenceReplay    = errors.New("replayed ESP sequence number")
	errSequenceTooOld    = errors.New("ESP sequence number outside replay window")
)

func marshalESPHeader(b []byte, spi, sequence uint32) {
	binary.BigEndian.PutUint32(b[:4], spi)
	binary.BigEndian.PutUint32(b[4:8], sequence)
}
