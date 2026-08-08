package ipsec

import (
	"crypto/rand"
	"encoding/binary"
)

// ESP packet layout (RFC 4303 tunnel mode):
//
//	[ SPI (4) | Seq (4) ] [ IV ] [ ciphertext: plaintext | padding | padLen | nextHeader ] [ ICV ]
//
// The AAD for AES-GCM is the 8-byte ESP header (RFC 4106 §3.1); for the CBC
// mode the integrity transform covers the whole packet up to (and including)
// the pad-length byte.

// Encapsulate wraps packet in an ESP frame and infers the inner protocol. The
// legacy target is an SA; the compatibility form accepts dst followed by SA.
func Encapsulate(packet []byte, target any, compatibility ...*SecurityAssociation) ([]byte, error) {
	dst, sa, err := encapsulationTarget(target, compatibility)
	if err != nil {
		return nil, err
	}
	var nextHeader byte
	switch {
	case len(packet) == 0:
		nextHeader = 0
	case packet[0]>>4 == 4:
		nextHeader = 4 // IPv4
	case packet[0]>>4 == 6:
		nextHeader = 41 // IPv6
	default:
		nextHeader = 0
	}
	return EncapsulateWithNextHeaderInto(dst, packet, nextHeader, sa)
}

func encapsulationTarget(target any, compatibility []*SecurityAssociation) ([]byte, *SecurityAssociation, error) {
	if sa, ok := target.(*SecurityAssociation); ok && len(compatibility) == 0 {
		return nil, sa, nil
	}
	if len(compatibility) != 1 {
		return nil, nil, errInvalidSA
	}
	if target == nil {
		return nil, compatibility[0], nil
	}
	dst, ok := target.([]byte)
	if !ok {
		return nil, nil, errInvalidSA
	}
	return dst, compatibility[0], nil
}

// EncapsulateInto is Encapsulate with the explicit next-header inference.
func EncapsulateInto(dst []byte, packet []byte, sa *SecurityAssociation) ([]byte, error) {
	var nextHeader byte
	if len(packet) > 0 && packet[0]>>4 == 4 {
		nextHeader = 4
	} else if len(packet) > 0 && packet[0]>>4 == 6 {
		nextHeader = 41
	}
	return EncapsulateWithNextHeaderInto(dst, packet, nextHeader, sa)
}

// EncapsulateWithNextHeaderInto wraps plaintext in an ESP frame with the
// given inner-protocol next header, writing into dst and returning the
// resulting slice.
func EncapsulateWithNextHeaderInto(dst []byte, plaintext []byte, nextHeader byte, sa *SecurityAssociation) ([]byte, error) {
	if sa == nil {
		return nil, errInvalidSA
	}
	if sa.EncryptionAlg == nil {
		return nil, errNoCipher
	}
	c, err := sa.cipher()
	if err != nil {
		return nil, err
	}
	if c == nil {
		return nil, errCipherUnavailable
	}

	ivSize := c.IVSize()
	total, padding, err := encapsulationLayout(len(plaintext), c.BlockSize(), ivSize, sa)
	if err != nil {
		return nil, err
	}

	// Sequence numbers start at 1 (field initialised to 0).
	seq, err := sa.reserveSequenceNumber()
	if err != nil {
		return nil, err
	}

	// Grow dst to fit the frame while preserving any caller-owned prefix.
	prefixLength := len(dst)
	required := prefixLength + total
	if cap(dst) < required {
		grown := make([]byte, len(dst), required)
		copy(grown, dst)
		dst = grown
	}
	out := dst

	// ESP header (SPI | Seq): the AAD for AES-GCM (RFC 4106 §3.1).
	hdr := make([]byte, 8)
	marshalESPHeader(hdr, sa.SPI, seq)
	out = append(out, hdr...)

	// IV: random bytes carried in clear.
	ivOff := len(out)
	out = append(out, make([]byte, ivSize)...)
	iv := out[ivOff : ivOff+ivSize]
	if _, err := rand.Read(iv); err != nil {
		return nil, err
	}

	// Payload: plaintext || padding(1..n) || padLen || nextHeader.
	payloadOff := len(out)
	out = append(out, plaintext...)
	for i := 0; i < padding; i++ {
		out = append(out, byte(i+1))
	}
	out = append(out, byte(padding))
	out = append(out, nextHeader)
	payload := out[payloadOff:]

	// Encrypt in place; the result appends the ciphertext (and tag for AEAD).
	out, err = c.Seal(out[:payloadOff], payload, iv, hdr)
	if err != nil {
		return nil, err
	}

	// CBC mode: compute the integrity over the whole packet and append.
	if !sa.IsAEAD && sa.IntegrityAlg2 != nil {
		out = append(out, sa.IntegrityAlg2.Compute(sa.IntegrityKey, out[prefixLength:])...)
	}
	return out, nil
}

// EncapsulationLayout returns the size of the encapsulated packet and the
// padding length for a plaintext of packetLen bytes.
func EncapsulationLayout(packetLen int, sa *SecurityAssociation) (total, padding int, err error) {
	if sa == nil {
		return 0, 0, errInvalidSA
	}
	if sa.EncryptionAlg == nil {
		return 0, 0, errNoCipher
	}
	c, err := sa.cipher()
	if err != nil {
		return 0, 0, err
	}
	return encapsulationLayout(packetLen, c.BlockSize(), c.IVSize(), sa)
}

// encapsulationLayout computes the total ESP frame size and the padding for a
// plaintext of plaintextLen bytes given the cipher's block size and IV size.
func encapsulationLayout(plaintextLen, blockSize, ivSize int, sa *SecurityAssociation) (total, padding int, err error) {
	if blockSize < 1 {
		return 0, 0, errInvalidBlockSize
	}
	padding = (blockSize - (plaintextLen+2)%blockSize) % blockSize
	total = 8 + ivSize + plaintextLen + 2 + padding
	total += sa.overhead()
	return total, padding, nil
}

// Decapsulate verifies and unwraps an ESP frame, appending the inner packet
// to dst. The next header byte is available via DecapsulateWithNextHeaderInto.
func Decapsulate(packet []byte, target any, compatibility ...*SecurityAssociation) ([]byte, error) {
	dst, sa, targetErr := encapsulationTarget(target, compatibility)
	if targetErr != nil {
		return nil, targetErr
	}
	inner, _, err := DecapsulateWithNextHeaderInto(dst, packet, sa)
	return inner, err
}

// DecapsulateWithNextHeaderInto verifies and unwraps an ESP frame, returning
// the inner packet (appended to dst) and the inner-protocol next header.
func DecapsulateWithNextHeaderInto(dst []byte, packet []byte, sa *SecurityAssociation) ([]byte, byte, error) {
	if len(packet) < 8 {
		return nil, 0, errPacketTooShort
	}
	if sa == nil {
		return nil, 0, errInvalidSA
	}
	if sa.EncryptionAlg == nil {
		return nil, 0, errNoCipher
	}
	c, err := sa.cipher()
	if err != nil {
		return nil, 0, err
	}
	if c == nil {
		return nil, 0, errCipherUnavailable
	}

	spi := binary.BigEndian.Uint32(packet[0:4])
	if sa.SPI != 0 && sa.SPI != spi {
		return nil, 0, errSPIMismatch
	}
	sequence := binary.BigEndian.Uint32(packet[4:8])
	if sequence == 0 {
		return nil, 0, errInvalidSequence
	}

	ivSize := c.IVSize()
	if len(packet) < 8+ivSize {
		return nil, 0, errIVTooShort
	}
	iv := packet[8 : 8+ivSize]

	var payload []byte
	if !sa.IsAEAD && sa.IntegrityAlg2 != nil {
		integSize := sa.IntegrityAlg2.OutputSize()
		if len(packet) < 8+ivSize+integSize {
			return nil, 0, errICVTooShort
		}
		// Verify the ICV over everything up to (not including) it.
		dataLen := len(packet) - integSize
		if !sa.IntegrityAlg2.Verify(sa.IntegrityKey, packet[:dataLen], packet[dataLen:]) {
			return nil, 0, errIntegrityFailed
		}
		payload = packet[8+ivSize : dataLen]
	} else {
		payload = packet[8+ivSize:]
	}

	// AAD is the 8-byte ESP header.
	plaintext, err := c.Open(nil, payload, iv, packet[:8])
	if err != nil {
		return nil, 0, err
	}
	if len(plaintext) < 2 {
		return nil, 0, errPayloadTooShort
	}
	padLen := int(plaintext[len(plaintext)-2])
	nextHeader := plaintext[len(plaintext)-1]
	if padLen+2 > len(plaintext) {
		return nil, 0, errBadPaddingLength
	}
	padding := plaintext[len(plaintext)-padLen-2 : len(plaintext)-2]
	for index, value := range padding {
		if value != byte(index+1) {
			return nil, 0, errInvalidPadding
		}
	}
	if err := sa.acceptInboundSequence(sequence); err != nil {
		return nil, 0, err
	}
	inner := plaintext[:len(plaintext)-padLen-2]
	return append(dst, inner...), nextHeader, nil
}
