package swu

import (
	"strings"

	"github.com/iniwex5/vowifi-go/engine/swu/eapaka"
)

// EAPReauthenticationState retains the protected state required by a later
// EAP-AKA or EAP-AKA' fast reauthentication exchange.
type EAPReauthenticationState struct {
	Identity            string
	Counter             uint16
	CounterOK           bool
	Keys                eapaka.Keys
	NextPseudonym       string
	Reauthenticated     bool
	CounterTooSmall     bool
	LastAcceptedCounter uint16
	LastRejectedCounter uint16
}

// Usable reports whether the identity and keys required for reauthentication
// are present.
func (s EAPReauthenticationState) Usable() bool {
	return strings.TrimSpace(s.Identity) != "" && len(s.Keys.KAut) > 0 && len(s.Keys.KEncr) > 0
}
