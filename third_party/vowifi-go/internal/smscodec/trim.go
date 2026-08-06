package smscodec

import (
	"encoding/hex"
	"strconv"
	"strings"
)

// ---------------------------------------------------------------------------
// TPDU layout helpers
//
// The functions below operate on the TPDU proper (the SMS-DELIVER octets after
// the SMSC number). Field offsets were recovered from the decompiled
// deliverTPDULayout:
//
//	[0]          TP-MTI (first octet; low 2 bits = message type, 0 = Deliver)
//	[1]          TP-OA address length
//	[2 .. 2+s)   TP-OA digits (s = (len+1)>>1, nibble packed)
//	[s+3]        TP-PID
//	[s+4]        TP-DCS
//	[s+5 .. s+11] TP-SCTS (7 octets)
//	[s+12]       TP-UDL
//	[s+13 ..]    TP-UD
// ---------------------------------------------------------------------------

// deliverTPDULayout validates a deliver TPDU and returns its parsing layout.
// dcs is the data-coding-scheme octet (0 when invalid); udSeptets is the
// TP-UDL (interpreted as septets when the alphabet is GSM-7).
func deliverTPDULayout(pdu []byte) (dcs byte, addrBytes, udStart, udLen int, udSeptets byte, ok bool) {
	if len(pdu) == 0 || pdu[0]&3 != 0 {
		return 0, 0, 0, 0, 0, false // must be an SMS-DELIVER first octet
	}
	if len(pdu) < 3 {
		return 0, 0, 0, 0, 0, false
	}
	addrBytes = (int(pdu[1]) + 1) >> 1 // octets occupied by the TP-OA digits
	if len(pdu) < addrBytes+3 || len(pdu) < addrBytes+0xd {
		return 0, 0, 0, 0, 0, false
	}

	dcs = pdu[addrBytes+4]
	udl := pdu[addrBytes+0xc]
	udSeptets = udl

	// Decode the alphabet from the DCS (3GPP TS 23.038). This is the exact
	// branch structure of the original, which is a slightly simplified mapping:
	switch {
	case dcs&0x80 == 0: // general data coding
		if g := (dcs >> 2) & 3; g == 3 {
			dcs = 0 // reserved group -> treat as GSM-7
		}
	case dcs&0xe0 == 0xc0: // reserved/8-bit range
		dcs = 0
	case dcs&0xf0 == 0xe0: // message class data coding
		dcs = 2 // UCS-2
	case dcs&0xf0 == 0xf0: // message class with 8-bit indicator
		if dcs&4 == 0 {
			dcs = 0
		} else {
			dcs = 1
		}
	default:
		return 0, 0, 0, 0, 0, false
	}

	// For GSM-7 (alphabet 0) the UDL is expressed in septets; the declared
	// payload is ceil(septets*7/8) octets.
	udLen = int(udl)
	if dcs == 0 {
		udLen = (int(udl)*7 + 7) >> 3
	}
	udStart = addrBytes + 0xd

	if udStart+udLen <= len(pdu) {
		return pdu[addrBytes+4], addrBytes, udStart, udLen, udSeptets, true
	}
	return 0, 0, 0, 0, 0, false
}

// DeliverTPDUDeclaredLength returns the user-data length the PDU declares in
// its TP-UDL header, i.e. addrBytes + 13 + userDataLen.
func DeliverTPDUDeclaredLength(pdu []byte) (int, bool) {
	_, addrBytes, _, udLen, _, ok := deliverTPDULayout(pdu)
	if !ok {
		return 0, false
	}
	declared := addrBytes + 0xd + udLen
	if len(pdu) < declared {
		return 0, false
	}
	return declared, true
}

// TrimDeliverTPDUToDeclaredLength truncates the PDU to the length declared in
// its own TP-UDL header. Some modems append garbage after the user data.
func TrimDeliverTPDUToDeclaredLength(pdu []byte) []byte {
	declared, ok := DeliverTPDUDeclaredLength(pdu)
	if ok && declared < len(pdu) {
		return pdu[:declared:declared]
	}
	return pdu
}

// normalizeDeliverTPDUGSM7SpareBits clears the unused high bits of the final
// GSM-7 user-data octet. Some modems leave these set, which breaks decoding.
func normalizeDeliverTPDUGSM7SpareBits(pdu []byte) []byte {
	_, _, udStart, udLen, udSeptets, ok := deliverTPDULayout(pdu)
	if !ok || udSeptets == 0 || udLen == 0 {
		return pdu
	}
	if len(pdu) < udStart {
		return pdu
	}
	spare := (int(udSeptets) * 7) % 8
	if spare == 0 {
		return pdu
	}
	last := udStart + udLen - 1
	mask := byte(1 << spare)
	if pdu[last]&^mask == 0 {
		return pdu // spare bits already clear
	}
	out := append([]byte(nil), pdu...)
	out[last] &= mask - 1
	return out
}

// ---------------------------------------------------------------------------
// AT command header handling
//
// Modems expose PDU hex strings prefixed by AT result headers such as
// "+CMT:<index>,". These helpers strip the header and trim the hex to the
// length the modem declared.
// ---------------------------------------------------------------------------

// ParseATSMSHeaderTPDULength extracts the TPDU length reported after the last
// "+CMT"/"+CDS"-style token in the AT header. Returns 0,false if none found.
func ParseATSMSHeaderTPDULength(header string) (int, bool) {
	i := strings.LastIndex(header, "+")
	if i < 0 || i+1 >= len(header) {
		return 0, false
	}
	rest := strings.TrimSpace(header[i+1:])
	// The header looks like "+CMT: <index>,<length>" or "+CMT: <length>".
	n, err := strconv.Atoi(rest)
	if err != nil {
		// try the segment after the first comma (message index)
		if c := strings.IndexByte(rest, ','); c >= 0 {
			n, err = strconv.Atoi(strings.TrimSpace(rest[c+1:]))
		}
	}
	if err != nil || n < 0 {
		return 0, false
	}
	return n, true
}

// TrimFullPDUHexByATHeader trims pduHex to the TPDU length found in the AT
// header, if the header reports one; otherwise the input is returned trimmed.
func TrimFullPDUHexByATHeader(pduHex, atHeader string) string {
	if declared, ok := ParseATSMSHeaderTPDULength(atHeader); ok {
		return TrimFullPDUHexByTPDULength(pduHex, declared)
	}
	return strings.TrimSpace(pduHex)
}

// TrimFullPDUHexByTPDULength trims the hex PDU string to declared octets.
// It accounts for the SCA length octet/address the modem includes.
func TrimFullPDUHexByTPDULength(pduHex string, declared int) string {
	pduHex = strings.TrimSpace(pduHex)
	if pduHex == "" || declared < 0 {
		return pduHex
	}
	raw, err := hex.DecodeString(pduHex)
	if err != nil || len(raw) == 0 {
		return pduHex
	}
	scaLen := int(raw[0])
	total := scaLen + declared + 1 // SCA len octet + SCA address + TPDU
	if total > 0 && total <= len(raw) {
		return strings.ToUpper(hex.EncodeToString(raw[:total]))
	}
	return strings.ToUpper(pduHex)
}
