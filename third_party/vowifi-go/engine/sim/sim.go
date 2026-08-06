// Package sim defines the AKA computation surface used by the vohive SIM
// providers (3GPP TS 33.102 / RFC 4187).
package sim

import "errors"

// AKAResult is the outcome of an AKA computation.
type AKAResult struct {
	RES  []byte // authentication response
	CK   []byte // cipher key
	IK   []byte // integrity key
	AUTS []byte // re-synchronisation token (on sync failure)
}

// AKAProvider computes AKA from the network challenge (RAND, AUTN).
type AKAProvider interface {
	CalculateAKA(rand16, autn16 []byte) (AKAResult, error)
}

// ErrSyncFailure is returned when AUTN indicates a synchronisation failure;
// the AUTS value is carried in AKAResult.AUTS.
var ErrSyncFailure = errors.New("sim: AKA synchronisation failure")

// ISIMAKAProvider computes AKA from the ISIM application (3GPP TS 31.103).
type ISIMAKAProvider interface {
	// CalculateISIMAKA computes the AKA result from the ISIM.
	CalculateISIMAKA(rand16, autn16 []byte) (AKAResult, error)
}
