package netstack

import (
	"context"
	"errors"

	"github.com/iniwex5/vowifi-go/internal/vowifi/ipsec3gpp"
)

func (n *Network) InstallIPSec3GPP(
	ctx context.Context,
	policy ipsec3gpp.Policy,
) (func() error, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if n == nil || n.bridge == nil {
		return nil, errors.New("netstack: packet bridge is not available")
	}
	transport, err := ipsec3gpp.NewTransport(policy)
	if err != nil {
		return nil, err
	}
	n.bridge.SetTransformer(transport)
	n.ipsecPolicyInstalled.Store(true)
	return func() error {
		n.bridge.SetTransformer(nil)
		n.ipsecPolicyInstalled.Store(false)
		return nil
	}, nil
}
