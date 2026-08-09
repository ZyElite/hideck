package dns

import (
	"context"
	"errors"
	"fmt"
	"net"
	"sort"
	"strconv"
	"strings"

	miekgdns "github.com/miekg/dns"
)

func discoverRegistrarFromServer(
	ctx context.Context,
	resolver queryResolver,
	domain, transport string,
	preferIPv6 bool,
) (string, error) {
	var failures []error
	if candidates, err := lookupSRVCandidates(ctx, resolver, "sip", transport, domain, preferIPv6); err == nil {
		return strings.Join(candidates, ","), nil
	} else {
		failures = append(failures, err)
	}
	if service, proto, name, ok, err := lookupNAPTRTarget(ctx, resolver, domain, transport); err == nil && ok {
		if candidates, srvErr := lookupSRVCandidates(ctx, resolver, service, proto, name, preferIPv6); srvErr == nil {
			return strings.Join(candidates, ","), nil
		} else {
			failures = append(failures, srvErr)
		}
	} else if err != nil {
		failures = append(failures, err)
	}
	addresses, err := lookupTargetAddresses(ctx, resolver, domain, preferIPv6)
	if err != nil {
		failures = append(failures, err)
		return "", errors.Join(failures...)
	}
	return strings.Join(formatEndpoints(addresses, defaultRegistrarSIPPort), ","), nil
}

func lookupSRVCandidates(
	ctx context.Context,
	resolver queryResolver,
	service, proto, domain string,
	preferIPv6 bool,
) ([]string, error) {
	queryCtx, cancel := context.WithTimeout(ctx, registrarQueryTimeout)
	defer cancel()
	_, records, err := resolver.LookupSRV(queryCtx, service, proto, domain)
	if err != nil {
		return nil, err
	}
	if len(records) == 0 {
		return nil, fmt.Errorf("dns: no SRV records for _%s._%s.%s", service, proto, domain)
	}
	sort.SliceStable(records, func(i, j int) bool { return lessSRV(records[i], records[j]) })
	candidates := make([]string, 0, len(records))
	seen := make(map[string]struct{})
	for _, record := range records {
		port := int(record.Port)
		if port == 0 {
			port = defaultRegistrarSIPPort
		}
		target := strings.TrimSuffix(strings.TrimSpace(record.Target), ".")
		addresses, resolveErr := lookupTargetAddresses(ctx, resolver, target, preferIPv6)
		if resolveErr != nil {
			addresses = []string{target}
		}
		appendUniqueEndpoints(&candidates, seen, addresses, port)
	}
	if len(candidates) == 0 {
		return nil, errors.New("dns: SRV records contained no registrar targets")
	}
	return candidates, nil
}

func lessSRV(left, right *net.SRV) bool {
	if left.Priority != right.Priority {
		return left.Priority < right.Priority
	}
	if left.Weight != right.Weight {
		return left.Weight > right.Weight
	}
	return left.Target < right.Target
}

func lookupNAPTRTarget(
	ctx context.Context,
	resolver queryResolver,
	domain, transport string,
) (string, string, string, bool, error) {
	queryCtx, cancel := context.WithTimeout(ctx, registrarQueryTimeout)
	defer cancel()
	records, err := resolver.LookupNAPTR(queryCtx, domain)
	if err != nil {
		return "", "", "", false, err
	}
	sort.SliceStable(records, func(i, j int) bool {
		if records[i].Order != records[j].Order {
			return records[i].Order < records[j].Order
		}
		return records[i].Preference < records[j].Preference
	})
	wanted := "SIP+D2U"
	if transport == "tcp" {
		wanted = "SIP+D2T"
	}
	replacement := selectNAPTRReplacement(records, wanted)
	service, proto, name, ok := parseSRVReplacement(replacement)
	return service, proto, name, ok, nil
}

func selectNAPTRReplacement(records []*miekgdns.NAPTR, wanted string) string {
	for _, record := range records {
		if strings.EqualFold(strings.TrimSpace(record.Service), wanted) && strings.TrimSpace(record.Replacement) != "" {
			return record.Replacement
		}
	}
	for _, record := range records {
		if strings.TrimSpace(record.Replacement) != "" {
			return record.Replacement
		}
	}
	return ""
}

func parseSRVReplacement(replacement string) (string, string, string, bool) {
	parts := strings.Split(strings.TrimSuffix(strings.TrimSpace(replacement), "."), ".")
	if len(parts) < 3 || !strings.HasPrefix(parts[0], "_") || !strings.HasPrefix(parts[1], "_") {
		return "", "", "", false
	}
	service := strings.TrimPrefix(parts[0], "_")
	proto := strings.TrimPrefix(parts[1], "_")
	name := strings.Join(parts[2:], ".")
	return service, proto, name, service != "" && proto != "" && name != ""
}

func lookupTargetAddresses(
	ctx context.Context,
	resolver queryResolver,
	host string,
	preferIPv6 bool,
) ([]string, error) {
	host = strings.TrimSuffix(strings.TrimSpace(host), ".")
	if host == "" {
		return nil, errors.New("dns: empty lookup target")
	}
	queryCtx, cancel := context.WithTimeout(ctx, registrarQueryTimeout)
	defer cancel()
	addresses, err := resolver.LookupIPAddr(queryCtx, host)
	if err != nil {
		return nil, err
	}
	ipv4 := make([]string, 0, len(addresses))
	globalIPv6 := make([]string, 0, len(addresses))
	otherIPv6 := make([]string, 0, len(addresses))
	seen := make(map[string]struct{})
	for _, address := range addresses {
		if address.IP == nil {
			continue
		}
		key := address.IP.String()
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		if address.IP.To4() != nil {
			ipv4 = append(ipv4, key)
		} else if address.IP.IsGlobalUnicast() {
			globalIPv6 = append(globalIPv6, key)
		} else {
			otherIPv6 = append(otherIPv6, key)
		}
	}
	if len(ipv4)+len(globalIPv6)+len(otherIPv6) == 0 {
		return nil, fmt.Errorf("dns: no address records for %s", host)
	}
	ipv6 := append(globalIPv6, otherIPv6...)
	if preferIPv6 {
		return append(ipv6, ipv4...), nil
	}
	return append(ipv4, ipv6...), nil
}

func formatEndpoints(hosts []string, port int) []string {
	result := make([]string, 0, len(hosts))
	appendUniqueEndpoints(&result, make(map[string]struct{}), hosts, port)
	return result
}

func appendUniqueEndpoints(result *[]string, seen map[string]struct{}, hosts []string, port int) {
	if port < 1 {
		return
	}
	for _, host := range hosts {
		host = strings.Trim(strings.TrimSpace(host), "[]")
		if host == "" {
			continue
		}
		endpoint := net.JoinHostPort(host, strconv.Itoa(port))
		if _, exists := seen[endpoint]; exists {
			continue
		}
		seen[endpoint] = struct{}{}
		*result = append(*result, endpoint)
	}
}
