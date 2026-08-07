package ipsec

import (
	"encoding/binary"
	"errors"
	"sync"

	"github.com/iniwex5/vowifi-go/engine/crypto"
)

// SecurityAssociation is a user-space ESP security association. It is used in
// two flavours (mirroring the decompiled NewSecurityAssociation /
// NewSecurityAssociationCBC constructors):
//
//   - AEAD: AES-GCM (RFC 4106). The encryption key is K|salt; the packet
//     carries an 8-byte IV, the GCM nonce is salt||IV and the tag is appended
//     by the AEAD.
//   - CBC: AES-CBC (raw, the ESP padding is applied by this package) plus a
//     separate integrity transform (e.g. HMAC-SHA1-96) covering the whole ESP
//     packet, whose output is appended as the ICV.
type SecurityAssociation struct {
	mu             sync.Mutex
	seqNo          uint32 // outbound ESP sequence number (first packet uses 1)
	inboundHighest uint32
	inboundBitmap  uint64
	spi            uint32 // local SPI, written into outbound ESP headers
	remoteSPI      uint32 // peer SPI, verified on inbound packets
	cipherName     uint16 // ENCR transform ID
	key            []byte // encryption key material (AES-GCM: K|salt)
	aead           bool   // true = AES-GCM (no separate integrity)
	integ          crypto.Integrity
	integKey       []byte
	prepared       crypto.PreparedCipher // lazily built by cipher()
}

const replayWindowSize = 64

// NewSecurityAssociation creates an AEAD-mode ESP SA. key is K|salt per
// RFC 4106 (the last 4 bytes are the GCM salt).
func NewSecurityAssociation(spi uint32, cipherName uint16, key []byte, remoteSPI uint32) *SecurityAssociation {
	return &SecurityAssociation{
		spi:        spi,
		cipherName: cipherName,
		key:        key,
		remoteSPI:  remoteSPI,
		aead:       true,
	}
}

// NewSecurityAssociationCBC creates a CBC-mode ESP SA with a separate
// integrity transform. integKey is the HMAC/XCBC key.
func NewSecurityAssociationCBC(spi uint32, cipherName uint16, key []byte, integ crypto.Integrity, integKey []byte, remoteSPI uint32) *SecurityAssociation {
	return &SecurityAssociation{
		spi:        spi,
		cipherName: cipherName,
		key:        key,
		integ:      integ,
		integKey:   integKey,
		remoteSPI:  remoteSPI,
	}
}

// NextSequenceNumber returns the next outbound ESP sequence number.
func (sa *SecurityAssociation) NextSequenceNumber() uint32 {
	sequence, _ := sa.reserveSequenceNumber()
	return sequence
}

func (sa *SecurityAssociation) reserveSequenceNumber() (uint32, error) {
	sa.mu.Lock()
	defer sa.mu.Unlock()
	if sa.seqNo == ^uint32(0) {
		return 0, errSequenceExhausted
	}
	sa.seqNo++
	return sa.seqNo, nil
}

func (sa *SecurityAssociation) acceptInboundSequence(sequence uint32) error {
	if sequence == 0 {
		return errInvalidSequence
	}
	sa.mu.Lock()
	defer sa.mu.Unlock()
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

// cipher returns the lazily-prepared encryption transform.
func (sa *SecurityAssociation) cipher() (crypto.PreparedCipher, error) {
	sa.mu.Lock()
	defer sa.mu.Unlock()
	if sa.prepared != nil {
		return sa.prepared, nil
	}
	if sa.cipherName == 0 {
		return nil, errNoCipher
	}
	c, err := crypto.PrepareCipher(sa.cipherName, sa.key)
	if err != nil {
		return nil, err
	}
	sa.prepared = c
	return c, nil
}

// overhead is the number of extra bytes appended by the encryption layer:
// the 16-byte GCM tag for AEAD, or the integrity output for CBC.
func (sa *SecurityAssociation) overhead() int {
	if sa.aead {
		return 16 // GCM tag (NewGCMWithNonceSize keeps the default 16-byte tag)
	}
	if sa.integ != nil {
		return sa.integ.OutputSize()
	}
	return 0
}

// Errors returned by the ESP layer.
var (
	errInvalidSA         = errors.New("invalid security association")
	errNoCipher          = errors.New("no cipher configured")
	errCipherUnavailable = errors.New("cipher not available")
	errSPIMismatch       = errors.New("SPI mismatch")
	errPacketTooShort    = errors.New("packet too short")
	errIntegrityFailed   = errors.New("integrity check failed")
	errPayloadTooShort   = errors.New("payload too short")
	errBadPaddingLength  = errors.New("bad padding length")
	errInvalidPadding    = errors.New("invalid ESP padding bytes")
	errInvalidBlockSize  = errors.New("invalid block size")
	errSequenceExhausted = errors.New("ESP sequence number exhausted")
	errInvalidSequence   = errors.New("invalid ESP sequence number zero")
	errSequenceReplay    = errors.New("replayed ESP sequence number")
	errSequenceTooOld    = errors.New("ESP sequence number outside replay window")
)

// marshalESPHeader writes SPI and sequence number in network byte order.
func marshalESPHeader(b []byte, spi, seq uint32) {
	binary.BigEndian.PutUint32(b[0:4], spi)
	binary.BigEndian.PutUint32(b[4:8], seq)
}
