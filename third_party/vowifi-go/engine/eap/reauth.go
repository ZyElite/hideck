package eap

import (
	"encoding/binary"
	"errors"
)

// SaveReauthData stores the re-authentication state from a full EAP-AKA
// authentication run, enabling fast re-authentication on the next exchange.
func (c *FastReauthContext) SaveReauthData(identity, reauthID, mk []byte) {
	c.available = true
	c.identity = append([]byte{}, identity...)
	c.reauthID = append([]byte{}, reauthID...)
	c.mk = append([]byte{}, mk...)
	c.counter = 0
}

// CanUseReauth reports whether fast re-authentication can be used: the context
// must hold re-authentication data and a non-empty re-auth identity.
func (c *FastReauthContext) CanUseReauth() bool {
	return c.available && len(c.reauthID) > 0
}

// BuildReauthResponse builds the EAP-AKA re-authentication response attribute
// bytes: AT_COUNTER (carrying the current counter), optionally
// AT_COUNTER_TOO_SMALL, and AT_MAC (the 16-byte MAC). The AT_MAC value is
// appended with its two reserved bytes zeroed; the caller fills in the actual
// MAC after the rest of the message is finalised (RFC 4187 §10.7).
func (c *FastReauthContext) BuildReauthResponse(counter uint16, counterTooSmall bool, mac []byte) ([]byte, error) {
	if len(mac) != 16 {
		return nil, errors.New("eap: AT_MAC must be 16 bytes")
	}
	c.counter = counter

	var out []byte
	// AT_COUNTER: Type(0x13) | Length(1) | Counter(2, big-endian).
	out = append(out, AttrATCounter, 0x01)
	out = binary.BigEndian.AppendUint16(out, counter)

	if counterTooSmall {
		// AT_COUNTER_TOO_SMALL: Type(0x14) | Length(1) | 2 reserved bytes.
		out = append(out, AttrATCounterTooSmall, 0x01, 0x00, 0x00)
	}

	// AT_MAC: Type(0x0B) | Length(5) | 2 reserved | 16 MAC.
	out = append(out, AttrATMAC, 0x05, 0x00, 0x00)
	out = append(out, mac...)
	return out, nil
}