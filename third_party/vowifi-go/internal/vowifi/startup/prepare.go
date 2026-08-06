// Package startup prepares the IMS identity at VoWiFi startup: it resolves
// the ISIM identity, normalises it and produces the prepared session consumed
// by the runtime host.
//
// Reconstructed from the decompiled internal/vowifi/startup.
package startup

import (
	"errors"

	"github.com/iniwex5/vowifi-go/runtimehost/identity"
)

// HasIMSIdentityResolution reports whether the prepared session resolved a
// usable IMS identity (IMPI + IMPU).
func HasIMSIdentityResolution(p identity.PreparedSession) bool {
	ident := p.IMSIdentity
	return ident.IMPI != "" && ident.IMPU != ""
}

// NormalizeIMSIdentity normalises the IMS identity fields.
func NormalizeIMSIdentity(ident identity.IMSIdentity) identity.IMSIdentity {
	ident.IMPI = trimSpace(ident.IMPI)
	ident.IMPU = trimSpace(ident.IMPU)
	ident.Domain = trimSpace(ident.Domain)
	return ident
}

// PrepareStart prepares the IMS identity and session profile for a VoWiFi
// start, delegating to the identity package.
func PrepareStart(input identity.PrepareStartInput) (identity.PreparedSession, error) {
	if input.Profile.IMSI == "" {
		return identity.PreparedSession{}, errors.New("startup: empty IMSI in profile")
	}
	return identity.PrepareStart(input)
}

// trimSpace trims surrounding whitespace.
func trimSpace(s string) string {
	start := 0
	end := len(s)
	for start < end && (s[start] == ' ' || s[start] == '\t' || s[start] == '\r' || s[start] == '\n') {
		start++
	}
	for end > start && (s[end-1] == ' ' || s[end-1] == '\t' || s[end-1] == '\r' || s[end-1] == '\n') {
		end--
	}
	return s[start:end]
}
