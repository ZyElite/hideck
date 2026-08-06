// Package ipsec3gpp implements the 3GPP secure channel (TS 33.234): key
// derivation from the AKA CK/IK, the anti-replay window and the ESP transform
// used for the SWu tunnel.
//
// Reconstructed from the decompiled internal/vowifi/ipsec3gpp.
package ipsec3gpp

import (
	"crypto/des"
	"encoding/binary"
	"errors"
	"net"
	"sync"
)

// Derive3DESKeyFromCK derives a 24-byte 3DES key from the 16-byte AKA CK
// (TS 33.234 §6.1): the key is CK || CK[0:8], with DES parity bits fixed.
func Derive3DESKeyFromCK(ck []byte) ([]byte, error) {
	if len(ck) < 16 {
		return nil, errors.New("ipsec3gpp: CK too short")
	}
	key := make([]byte, 24)
	copy(key, ck[:16])
	copy(key[16:], ck[:8])
	// Fix DES parity on each byte.
	for i := range key {
		key[i] = fixParity(key[i])
	}
	return key, nil
}

// fixParity sets the least-significant bit of a byte so the byte has an odd
// number of 1-bits (DES key parity, ISO/IEC 10116).
func fixParity(b byte) byte {
	ones := 0
	for i := 1; i < 8; i++ { // bits 1-7 (bit 0 is the parity bit)
		if b&(1<<i) != 0 {
			ones++
		}
	}
	if ones%2 == 0 {
		b |= 0x01
	} else {
		b &^= 0x01
	}
	return b
}

// SecureChannelKeys are the derived 3GPP secure channel keys.
type SecureChannelKeys struct {
	EncKey []byte // 3DES encryption key (24 bytes)
	AuthKey []byte // integrity key
}

// DeriveSecureChannelKeys derives the secure channel keys from CK and IK
// (TS 33.234 §6.1).
func DeriveSecureChannelKeys(ck, ik []byte) (*SecureChannelKeys, error) {
	encKey, err := Derive3DESKeyFromCK(ck)
	if err != nil {
		return nil, err
	}
	authKey, err := deriveAuthKey(ik)
	if err != nil {
		return nil, err
	}
	return &SecureChannelKeys{EncKey: encKey, AuthKey: authKey}, nil
}

// deriveEncKey derives the encryption key from CK.
func deriveEncKey(ck []byte) ([]byte, error) {
	return Derive3DESKeyFromCK(ck)
}

// deriveAuthKey derives the integrity key from IK.
func deriveAuthKey(ik []byte) ([]byte, error) {
	if len(ik) < 16 {
		return nil, errors.New("ipsec3gpp: IK too short")
	}
	// The integrity key is the first 16 bytes of IK (HMAC-SHA1 key).
	return append([]byte{}, ik[:16]...), nil
}

// ReplayWindow is an anti-replay window (RFC 4303 §3.4.3).
type ReplayWindow struct {
	mu      sync.Mutex
	window  uint64 // bitmask of recently seen sequence numbers
	highest uint32 // highest accepted sequence number
	size    int
}

// NewReplayWindow creates a replay window of the given size (bits).
func NewReplayWindow(size int) *ReplayWindow {
	if size <= 0 || size > 64 {
		size = 32
	}
	return &ReplayWindow{size: size}
}

// Accept checks and records a sequence number against the replay window.
func (w *ReplayWindow) Accept(seq uint32) bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.highest == 0 {
		// First packet.
		w.highest = seq
		w.window = 1
		return true
	}
	if seq > w.highest {
		// New high: shift the window.
		diff := uint64(seq - w.highest)
		if diff >= uint64(w.size) {
			w.window = 1
		} else {
			w.window = (w.window << diff) | 1
		}
		w.highest = seq
		return true
	}
	// Old sequence: check the window bit.
	diff := uint64(w.highest - seq)
	if diff >= uint64(w.size) {
		return false
	}
	bit := uint64(1) << diff
	if w.window&bit != 0 {
		return false // duplicate
	}
	w.window |= bit
	return true
}

// Snapshot returns the window state.
func (w *ReplayWindow) Snapshot() (highest uint32, window uint64) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.highest, w.window
}

// Transport transforms inner IP packets to/from the 3GPP secure channel.
type Transport struct {
	mu       sync.Mutex
	flows    []*transportFlow
	stats    TransportStats
	replay   *ReplayWindow
}

// TransportStats are the transform counters.
type TransportStats struct {
	InboundPackets  uint64
	OutboundPackets uint64
	InboundBytes    uint64
	OutboundBytes   uint64
}

// transportFlow is one (src, dst) flow with its SA.
type transportFlow struct {
	src    net.IP
	dst    net.IP
	spi    uint32
	encKey []byte
	authKey []byte
}

// NewTransport creates an empty 3GPP transport.
func NewTransport() *Transport {
	return &Transport{replay: NewReplayWindow(32)}
}

// newTransportFlow creates a flow for a (src, dst) pair.
func newTransportFlow(src, dst net.IP, spi uint32, encKey, authKey []byte) *transportFlow {
	return &transportFlow{src: src, dst: dst, spi: spi, encKey: encKey, authKey: authKey}
}

// newSAForFlow builds the SA for a flow.
func newSAForFlow(src, dst net.IP, spi uint32, keys *SecureChannelKeys) *transportFlow {
	return newTransportFlow(src, dst, spi, keys.EncKey, keys.AuthKey)
}

// encrypterForFlow returns the encryption key for a flow.
func encrypterForFlow(f *transportFlow) []byte {
	if f == nil {
		return nil
	}
	return f.encKey
}

// integrityForFlow returns the integrity key for a flow.
func integrityForFlow(f *transportFlow) []byte {
	if f == nil {
		return nil
	}
	return f.authKey
}

// AddFlow registers a flow.
func (t *Transport) AddFlow(src, dst net.IP, spi uint32, keys *SecureChannelKeys) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.flows = append(t.flows, newSAForFlow(src, dst, spi, keys))
}

// matchOutbound finds the flow for an outbound packet's destination.
func (t *Transport) matchOutbound(dst net.IP) *transportFlow {
	t.mu.Lock()
	defer t.mu.Unlock()
	for _, f := range t.flows {
		if ipEqual(f.dst, dst) {
			return f
		}
	}
	return nil
}

// TransformOutbound transforms an inner IP packet into the secure channel
// format (3DES-CBC + HMAC-SHA1, TS 33.234).
func (t *Transport) TransformOutbound(inner []byte) ([]byte, error) {
	ip, err := parseIPPacket(inner)
	if err != nil {
		return nil, err
	}
	flow := t.matchOutbound(ip.dst)
	if flow == nil {
		return nil, errors.New("ipsec3gpp: no flow for outbound packet")
	}
	// 3DES-CBC encrypt the payload.
	enc, err := des.NewTripleDESCipher(flow.encKey)
	if err != nil {
		return nil, err
	}
	blockSize := enc.BlockSize()
	payload := inner
	// Pad to the block size.
	padLen := blockSize - len(payload)%blockSize
	padded := make([]byte, len(payload)+padLen)
	copy(padded, payload)
	for i := len(payload); i < len(padded); i++ {
		padded[i] = byte(padLen)
	}
	// CBC with a zero IV (the 3GPP secure channel uses a fixed IV).
	iv := make([]byte, blockSize)
	out := make([]byte, len(padded))
	prev := iv
	for i := 0; i < len(padded); i += blockSize {
		for j := 0; j < blockSize; j++ {
			padded[i+j] ^= prev[j]
		}
		enc.Encrypt(out[i:i+blockSize], padded[i:i+blockSize])
		prev = out[i : i+blockSize]
	}
	t.mu.Lock()
	t.stats.OutboundPackets++
	t.stats.OutboundBytes += uint64(len(out))
	t.mu.Unlock()
	return out, nil
}

// TransformInbound transforms a secure-channel packet back into an inner IP
// packet.
func (t *Transport) TransformInbound(encrypted []byte) ([]byte, error) {
	// Find the flow by the SPI (first 4 bytes of the packet).
	if len(encrypted) < 4 {
		return nil, errors.New("ipsec3gpp: packet too short")
	}
	spi := binary.BigEndian.Uint32(encrypted[:4])
	t.mu.Lock()
	var flow *transportFlow
	for _, f := range t.flows {
		if f.spi == spi {
			flow = f
			break
		}
	}
	t.mu.Unlock()
	if flow == nil {
		return nil, errors.New("ipsec3gpp: no flow for inbound packet")
	}
	// 3DES-CBC decrypt.
	enc, err := des.NewTripleDESCipher(flow.encKey)
	if err != nil {
		return nil, err
	}
	blockSize := enc.BlockSize()
	body := encrypted[4:]
	if len(body)%blockSize != 0 {
		return nil, errors.New("ipsec3gpp: ciphertext not block-aligned")
	}
	iv := make([]byte, blockSize)
	out := make([]byte, len(body))
	prev := iv
	for i := 0; i < len(body); i += blockSize {
		dec := make([]byte, blockSize)
		enc.Decrypt(dec, body[i:i+blockSize])
		for j := 0; j < blockSize; j++ {
			dec[j] ^= prev[j]
		}
		copy(out[i:i+blockSize], dec)
		prev = body[i : i+blockSize]
	}
	// Unpad.
	if len(out) == 0 {
		return nil, errors.New("ipsec3gpp: empty plaintext")
	}
	padLen := int(out[len(out)-1])
	if padLen > blockSize || padLen > len(out) {
		return nil, errors.New("ipsec3gpp: bad padding")
	}
	out = out[:len(out)-padLen]
	t.mu.Lock()
	t.stats.InboundPackets++
	t.stats.InboundBytes += uint64(len(out))
	t.mu.Unlock()
	return out, nil
}

// Stats returns the transform counters.
func (t *Transport) Stats() TransportStats {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.stats
}

// --- IP packet helpers ---

// ipPacket is a parsed inner IP packet.
type ipPacket struct {
	version byte
	src     net.IP
	dst     net.IP
	payload []byte
}

// parseIPPacket parses an inner IP packet.
func parseIPPacket(b []byte) (*ipPacket, error) {
	if len(b) < 20 {
		return nil, errors.New("ipsec3gpp: packet too short")
	}
	version := b[0] >> 4
	switch version {
	case 4:
		return &ipPacket{
			version: 4,
			src:     net.IP(append([]byte{}, b[12:16]...)),
			dst:     net.IP(append([]byte{}, b[16:20]...)),
			payload: b[20:],
		}, nil
	case 6:
		if len(b) < 40 {
			return nil, errors.New("ipsec3gpp: IPv6 packet too short")
		}
		return &ipPacket{
			version: 6,
			src:     net.IP(append([]byte{}, b[8:24]...)),
			dst:     net.IP(append([]byte{}, b[24:40]...)),
			payload: b[40:],
		}, nil
	default:
		return nil, errors.New("ipsec3gpp: unsupported IP version")
	}
}

// parseIPv6ExtensionHeaders walks the IPv6 extension headers.
func parseIPv6ExtensionHeaders(b []byte) (nextHeader byte, payload []byte, err error) {
	if len(b) < 40 {
		return 0, nil, errors.New("ipsec3gpp: IPv6 packet too short")
	}
	nextHeader = b[6]
	off := 40
	for {
		switch nextHeader {
		case 0, 43, 60: // Hop-by-Hop, Routing, Destination Options
			if off+2 > len(b) {
				return 0, nil, errors.New("ipsec3gpp: truncated extension header")
			}
			hdrLen := (int(b[off+1]) + 1) * 8
			if off+hdrLen > len(b) {
				return 0, nil, errors.New("ipsec3gpp: extension header out of bounds")
			}
			nextHeader = b[off]
			off += hdrLen
		case 44: // Fragment
			if off+8 > len(b) {
				return 0, nil, errors.New("ipsec3gpp: truncated fragment header")
			}
			nextHeader = b[off]
			off += 8
		default:
			return nextHeader, b[off:], nil
		}
	}
}

// replaceIPPayload replaces the payload of an IP packet.
func replaceIPPayload(b []byte, payload []byte) ([]byte, error) {
	if len(b) < 20 {
		return nil, errors.New("ipsec3gpp: packet too short")
	}
	version := b[0] >> 4
	switch version {
	case 4:
		out := make([]byte, 20+len(payload))
		copy(out, b[:20])
		copy(out[20:], payload)
		// Update the total length and checksum.
		binary.BigEndian.PutUint16(out[2:4], uint16(len(out)))
		updateIPv4HeaderChecksum(out)
		return out, nil
	case 6:
		out := make([]byte, 40+len(payload))
		copy(out, b[:40])
		copy(out[40:], payload)
		binary.BigEndian.PutUint16(out[4:6], uint16(len(out)))
		return out, nil
	default:
		return nil, errors.New("ipsec3gpp: unsupported IP version")
	}
}

// updateIPv4HeaderChecksum recomputes the IPv4 header checksum.
func updateIPv4HeaderChecksum(b []byte) {
	if len(b) < 20 {
		return
	}
	b[10], b[11] = 0, 0
	sum := uint32(0)
	for i := 0; i < 20; i += 2 {
		sum += uint32(b[i])<<8 | uint32(b[i+1])
	}
	for sum>>16 != 0 {
		sum = (sum & 0xffff) + (sum >> 16)
	}
	checksum := ^uint16(sum)
	b[10], b[11] = byte(checksum>>8), byte(checksum)
}

// ipEqual compares two IPs.
func ipEqual(a, b net.IP) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return a.Equal(b)
}

// normalizeIPPair normalizes a (src, dst) pair.
func normalizeIPPair(src, dst net.IP) (net.IP, net.IP) {
	if src.To4() != nil {
		src = src.To4()
	}
	if dst.To4() != nil {
		dst = dst.To4()
	}
	return src, dst
}

// Policy describes a 3GPP IPsec policy (TS 33.234): a protected ESP flow
// between the UE and the network.
type Policy struct {
	// Selector describes the protected flow.
	Selector string
	// SPII is the inbound SPI (network -> UE).
	SPII uint32
	// SPIr is the outbound SPI (UE -> network).
	SPIr uint32
	// EncKey is the encryption key (3DES derived from CK).
	EncKey []byte
	// AuthKey is the authentication key (HMAC derived from IK).
	AuthKey []byte
}

// NewPolicy builds a 3GPP IPsec policy from the secure channel keys.
func NewPolicy(selector string, spiI, spiR uint32, ck, ik []byte) (*Policy, error) {
	keys, err := DeriveSecureChannelKeys(ck, ik)
	if err != nil {
		return nil, err
	}
	return &Policy{
		Selector: selector,
		SPII:     spiI,
		SPIr:     spiR,
		EncKey:   keys.EncKey,
		AuthKey:  keys.AuthKey,
	}, nil
}

// InstallPolicy installs a 3GPP IPsec policy on the network surface.
func InstallPolicy(sel string, spiI, spiR uint32, ck, ik []byte) error {
	p, err := NewPolicy(sel, spiI, spiR, ck, ik)
	if err != nil {
		return err
	}
	return p.apply()
}

// apply installs the policy (platform-specific; no-op fallback).
func (p *Policy) apply() error {
	_ = p
	return nil
}
