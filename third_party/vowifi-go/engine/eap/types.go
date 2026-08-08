// Package eap implements the EAP and EAP-AKA packet surface used by SWu.
package eap

const (
	CodeRequest  uint8 = 1
	CodeResponse uint8 = 2
	CodeSuccess  uint8 = 3
	CodeFailure  uint8 = 4
)

const (
	TypeIdentity     uint8 = 1
	TypeNAK          uint8 = 3
	TypeNotification uint8 = 4
	TypeAKA          uint8 = 23
	TypeAKAPrime     uint8 = 50
)

const (
	SubtypeChallenge        uint8 = 1
	SubtypeAuthReject       uint8 = 2
	SubtypeSyncFailure      uint8 = 4
	SubtypeIdentity         uint8 = 5
	SubtypeNotification     uint8 = 12
	SubtypeReauthentication uint8 = 13
	SubtypeClientError      uint8 = 14
)

// Compatibility names retained from the later reconstruction.
const (
	SubtypeAKAChallenge  = SubtypeChallenge
	SubtypeAKAAuthReject = SubtypeAuthReject
)

const (
	AT_RAND              uint8 = 1
	AT_AUTN              uint8 = 2
	AT_RES               uint8 = 3
	AT_AUTS              uint8 = 4
	AT_PADDING           uint8 = 6
	AT_NONCE_MT          uint8 = 7
	AT_PERMANENT_ID_REQ  uint8 = 10
	AT_MAC               uint8 = 11
	AT_NOTIFICATION      uint8 = 12
	AT_ANY_ID_REQ        uint8 = 13
	AT_IDENTITY          uint8 = 14
	AT_VERSION_LIST      uint8 = 15
	AT_SELECTED_VERSION  uint8 = 16
	AT_FULLAUTH_ID_REQ   uint8 = 17
	AT_COUNTER           uint8 = 19
	AT_COUNTER_TOO_SMALL uint8 = 20
	AT_NONCE_S           uint8 = 21
	AT_CLIENT_ERROR_CODE uint8 = 22
	AT_KDF_INPUT         uint8 = 23
	AT_KDF               uint8 = 24
	AT_IV                uint8 = 129
	AT_ENCR_DATA         uint8 = 130
	AT_NEXT_PSEUDONYM    uint8 = 132
	AT_NEXT_REAUTH_ID    uint8 = 133
	AT_CHECKCODE         uint8 = 134
	AT_RESULT_IND        uint8 = 135
	AT_BIDDING           uint8 = 136
)

// Compatibility names retained for existing callers in this tree.
const (
	AttrATRAND            = AT_RAND
	AttrATAUTN            = AT_AUTN
	AttrATRES             = AT_RES
	AttrATPadding         = AT_PADDING
	AttrATPermanentIDReq  = AT_PERMANENT_ID_REQ
	AttrATMAC             = AT_MAC
	AttrATNotification    = AT_NOTIFICATION
	AttrATClientErrorCode = AT_CLIENT_ERROR_CODE
	AttrATIdentity        = AT_IDENTITY
	AttrATVersionList     = AT_VERSION_LIST
	AttrATSelectedVersion = AT_SELECTED_VERSION
	AttrATCounter         = AT_COUNTER
	AttrATCounterTooSmall = AT_COUNTER_TOO_SMALL
)

type EAPPacket struct {
	Code       uint8
	Identifier uint8
	Type       uint8
	Subtype    uint8
	Data       []byte
}

type Attribute struct {
	Type   uint8
	Length uint8
	Value  []byte
}

type EAPAttribute = Attribute

type ReauthState struct {
	NextReauthID string
	Counter      uint16
	MK           []byte
	KEncr        []byte
	KAut         []byte
	MSK          []byte
	EMSK         []byte
}

type FastReauthContext struct {
	Enabled      bool
	ReauthID     string
	Counter      uint16
	NonceS       []byte
	CounterSmall bool
	KEncr        []byte
	KAut         []byte
	MK           []byte
}
