package imscore

import (
	"context"
	"errors"

	"github.com/iniwex5/vowifi-go/internal/vowifi/ipsec3gpp"
)

// IPSec3GPPInstaller installs an IMS transport policy and returns its cleanup.
type IPSec3GPPInstaller interface {
	InstallIPSec3GPP(context.Context, ipsec3gpp.Policy) (func() error, error)
}

// MissingIPSec3GPPInstaller exposes a missing installer as an explicit error.
type MissingIPSec3GPPInstaller struct{}

func (MissingIPSec3GPPInstaller) InstallIPSec3GPP(
	context.Context,
	ipsec3gpp.Policy,
) (func() error, error) {
	return nil, errors.New("ipsec3gpp: installer not configured")
}

// SystemIPSec3GPPInstaller installs policies through the host kernel.
type SystemIPSec3GPPInstaller struct{}

func (SystemIPSec3GPPInstaller) InstallIPSec3GPP(
	ctx context.Context,
	policy ipsec3gpp.Policy,
) (func() error, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return ipsec3gpp.InstallPolicy(policy)
}
