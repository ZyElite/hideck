package ikev2

import "fmt"

const (
	// NotifyTypeEAPOnlyAuthentication requests RFC 5998 EAP-only responder authentication.
	NotifyTypeEAPOnlyAuthentication uint16 = 16417
	// NotifyTypeRekeySA identifies the CHILD_SA replaced by CREATE_CHILD_SA.
	NotifyTypeRekeySA uint16 = 16393
)

// notifyNames maps IKEv2 notify message types (RFC 7296 §3.10.1 plus common
// extensions) to their string names. Recovered from the decompiled
// notify_names.go table.
var notifyNames = map[uint16]string{
	1:     "INVALID_SA_PROPOSAL",
	2:     "PEER_NOT_SUPPORTED",
	4:     "RETRY_IN_USE",
	5:     "SUPPORTED_CRITICAL_PAYLOAD",
	14:    "NO_PROPOSAL_CHOSEN",
	18:    "INVALID_MAJOR_VERSION",
	19:    "INVALID_SYNTAX",
	20:    "INVALID_MESSAGE_ID",
	21:    "INVALID_SPI",
	22:    "NO_PROPOSAL_CHOSEN",
	34:    "NO_ADDITIONAL_SAS",
	35:    "INVALID_KE_PAYLOAD",
	38:    "INVALID_CERT_AUTHORITY",
	40:    "INVALID_CONFIGURATION",
	16388: "NAT_DETECTION_SOURCE_IP",
	16389: "NAT_DETECTION_DESTINATION_IP",
	16390: "COOKIE",
	16391: "USE_TRANSPORT_MODE",
	16392: "HTTP_CERT_LOOKUP_SUPPORTED",
	16393: "REKEY_SA",
	16394: "ESP_TFC_PADDING_NOT_SUPPORTED",
	16395: "NON_FIRST_FRAGMENTS_ALSO",
	16396: "MOBIKE_SUPPORTED",
	16397: "ADDITIONAL_IP4_ADDRESS",
	16398: "ADDITIONAL_IP6_ADDRESS",
	16399: "NO_ADDITIONAL_ADDRESSES",
	16400: "UPDATE_SA_ADDRESSES",
	16401: "COOKIE2",
	16402: "NO_NATS_ALLOWED",
	16403: "PAD_LENGTH",
	16404: "REDIRECT_SUPPORTED",
	16405: "SIGNATURE_HASH_ALGORITHMS",
	16406: "REDIRECTED_FROM",
	16407: "REDIRECTED_TO",
	16386: "USE_AGGFRAG",
	16409: "ADDITIONAL_TS_POSSIBLE",
	16410: "IPCOMP_SUPPORTED",
	16411: "IP4_ALLOWED",
	16412: "IP6_ALLOWED",
	16385: "INITIAL_CONTACT",
	16387: "SET_WINDOW_SIZE",
	16413: "PSK_PERSIST",
	16414: "PSK_CONFIRM",
	16415: "FAILED_CP_REQUIRED",
	16416: "SECURE_PASSWORD_METHODS",
	16417: "EAP_ONLY_AUTHENTICATION",
	16418: "UNEXPECTED_NAT_DETECTED",
	16419: "REGISTRATION_FAILED",
}

// NotifyTypeToString returns the name of an IKEv2 notify message type, or a
// decimal fallback for unknown types.
func NotifyTypeToString(t uint16) string {
	if name, ok := notifyNames[t]; ok {
		return name
	}
	return fmt.Sprintf("%d", t)
}
