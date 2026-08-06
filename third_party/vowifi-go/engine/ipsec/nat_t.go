package ipsec

// parseIKEPayload reports whether b carries an IKE packet and returns the IKE
// packet slice. IKE/ESP demultiplexing follows RFC 3948:
//
//   - A UDP datagram whose first 4 bytes are the non-ESP marker (0x00 00 00 00)
//     is the IKE packet that follows the marker.
//   - Otherwise a packet whose version/exchange-type bytes match IKEv2
//     (version 0x20, exchange type 34..37) is an unmarked IKE packet; any
//     other packet is ESP.
//
// origCap is the capacity of the original buffer so the returned slice header
// stays consistent.
func parseIKEPayload(b []byte, origCap int) ([]byte, bool) {
	_ = origCap // preserved for signature fidelity (slice cap already adjusted)
	if len(b) < 4 {
		return nil, false
	}
	nonZeroSPI := b[0] != 0 || b[1] != 0 || b[2] != 0 || b[3] != 0
	if nonZeroSPI {
		// ESP, or an unmarked IKE packet (IKE_AUTH response etc.).
		if len(b) < 28 {
			return nil, false
		}
		if b[17] == 0x20 && b[18] >= 0x22 && b[18] <= 0x25 {
			return b, true
		}
		return nil, false
	}
	// Non-ESP marker: the IKE packet follows the 4 zero bytes.
	if len(b) < 32 {
		return nil, false
	}
	if b[21] == 0x20 && b[22] >= 0x22 && b[22] <= 0x25 {
		return b[4:], true
	}
	return nil, false
}
