package access

import (
	enginesim "github.com/iniwex5/vowifi-go/engine/sim"
	"github.com/iniwex5/vowifi-go/internal/vowifi/profile"
	"github.com/iniwex5/vowifi-go/internal/vowifi/simauth"
)

// Capabilities describes the identity and AKA facilities exposed by an access adapter.
type Capabilities struct {
	SIM          bool
	ISIMIdentity bool
	ISIMAKA      bool
	Modem        bool
	Reader       bool
}

// Adapter exposes modem capabilities used while preparing a runtime session.
type Adapter interface {
	Capabilities() Capabilities
	IMSIdentityProvider() profile.Provider
}

// SIMAdapter selects the SIM application used independently by SWu and IMS.
type SIMAdapter interface {
	EPDGSIMProvider(profile.AuthPlan) enginesim.AKAProvider
	IMSAKAProvider(profile.AuthPlan) simauth.AKAProvider
	IMSIdentityProvider() profile.Provider
}
