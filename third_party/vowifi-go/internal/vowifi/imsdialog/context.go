// Package imsdialog contains the immutable registration view used to build
// IMS voice dialog messages.
package imsdialog

import "github.com/emiago/sipgo/sip"

// Context is the v1.5.5 dialog-building context.
type Context struct {
	IMPU                   string
	Realm                  string
	ContactID              string
	LocalAddr              string
	LocalPortC             int
	LocalPortS             int
	Transport              string
	ServiceRoute           string
	SecVerify              string
	SecMode                string
	PAccessNetworkInfo     string
	UserAgent              string
	IMEI                   string
	PubGRUU                string
	DeviceID               string
	CachedFromURI          sip.Uri
	CachedContactHdr       *sip.ContactHeader
	CachedRouteHdr         sip.Header
	CachedSecVerifyHdr     sip.Header
	CachedPANIHdr          sip.Header
	CachedPPIHdr           sip.Header
	VoiceSupportedHeader   string
	VoiceAllowHeader       string
	VoiceAcceptContact     string
	VoicePPreferredService string
	VoiceAccessType        string
	VoiceContactParamOrder []string
}
