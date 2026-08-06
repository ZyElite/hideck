// Package eap implements the EAP packet framing and EAP-AKA/AKA' attribute
// handling used by the vowifi SWu client.
//
// Reconstructed from the decompiled engine/eap. It covers RFC 3748 (EAP
// packet framing), RFC 4187 (EAP-AKA attributes) and the fast re-authentication
// context (RFC 4187 §5).
package eap

// EAP packet codes (RFC 3748 §4.1).
const (
	CodeRequest     byte = 1
	CodeResponse    byte = 2
	CodeSuccess     byte = 3
	CodeFailure     byte = 4
)

// EAP types used by the SWu client.
const (
	TypeIdentity     byte = 1  // EAP-Identity
	TypeAKA          byte = 23 // 0x17, EAP-AKA (RFC 4187)
	TypeAKAPrime     byte = 50 // 0x32, EAP-AKA' (RFC 5448)
	TypeNotification byte = 4
	TypeNAK          byte = 3
)

// EAP-AKA/AKA' subtypes (RFC 4187 §8.1).
const (
	SubtypeAKAChallenge     byte = 0x01
	SubtypeAKAAuthReject    byte = 0x02
	SubtypeSyncFailure      byte = 0x04
	SubtypeIdentity         byte = 0x05
	SubtypeNotification     byte = 0x0C
	SubtypeClientError      byte = 0x0E
	SubtypeReauthentication byte = 0x02
)

// EAP-AKA attribute types (RFC 4187 §9).
const (
	AttrATRAND            byte = 0x01
	AttrATAUTN            byte = 0x02
	AttrATRES             byte = 0x03
	AttrATPadding         byte = 0x04
	AttrATPermanentIDReq  byte = 0x0A
	AttrATMAC             byte = 0x0B
	AttrATNotification    byte = 0x0C
	AttrATClientErrorCode byte = 0x0D
	AttrATIdentity        byte = 0x0E
	AttrATVersionList     byte = 0x10
	AttrATSelectedVersion byte = 0x11
	AttrATCounter         byte = 0x13
	AttrATCounterTooSmall byte = 0x14
)

// EAPPacket is a parsed EAP packet (RFC 3748). For Request/Response of type
// EAP-AKA/AKA' the 8-byte AKA header (Code|ID|Len|Type|SubType|2 reserved) is
// decoded into Type/SubType and the remaining bytes into Data; for other
// Request/Response types the 5-byte header leaves the type-specific data in
// Data; Success/Failure carry no data.
type EAPPacket struct {
	Code       byte
	Identifier byte
	Type       byte
	SubType    byte // valid for EAP-AKA/AKA'
	Data       []byte
}

// EAPAttribute is one EAP-AKA attribute (RFC 4187 §9). Length is in 4-byte
// words; Value is the (Length*4 - 2) payload bytes.
type EAPAttribute struct {
	Type   byte
	Length byte // in 4-byte words
	Value  []byte
}

// FastReauthContext holds the state needed for EAP-AKA fast re-authentication
// (RFC 4187 §5).
type FastReauthContext struct {
	available bool
	identity  []byte
	reauthID  []byte
	counter   uint16
	mk        []byte // re-authentication master key
	data      []byte // additional preserved reauth data
}