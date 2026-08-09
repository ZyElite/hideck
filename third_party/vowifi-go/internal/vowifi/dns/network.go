package dns

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
)

// RegistrarNetwork is the IMS network capability needed for registrar SRV lookup.
type RegistrarNetwork interface {
	LookupSRV(context.Context, string, string, string) (string, uint16, error)
}

// DiscoverRegistrarViaNetwork applies the legacy transport order to an IMS network resolver.
func DiscoverRegistrarViaNetwork(
	ctx context.Context,
	domain, transport string,
	network RegistrarNetwork,
) (string, error) {
	domain = strings.TrimSpace(domain)
	if domain == "" {
		return "", errors.New("dns: empty registrar domain")
	}
	if network == nil {
		return "", errors.New("dns: nil IMS registrar network")
	}
	var failures []error
	for _, candidate := range registrarTransportCandidates(transport) {
		host, port, err := network.LookupSRV(ctx, "sip", candidate, domain)
		if err != nil {
			if contextErr := ctx.Err(); contextErr != nil {
				return "", fmt.Errorf("dns: registrar SRV lookup canceled: %w", contextErr)
			}
			failures = append(failures, fmt.Errorf("_%s: %w", candidate, err))
			continue
		}
		host = strings.TrimSuffix(strings.TrimSpace(host), ".")
		if host == "" || port == 0 {
			failures = append(failures, fmt.Errorf("_%s: empty SRV target", candidate))
			continue
		}
		return net.JoinHostPort(host, strconv.Itoa(int(port))), nil
	}
	// RFC 3263 falls back to the domain's address records when SRV is absent.
	if len(failures) > 0 {
		return net.JoinHostPort(domain, strconv.Itoa(defaultRegistrarSIPPort)), nil
	}
	return "", errors.New("dns: registrar discovery produced no candidates")
}
