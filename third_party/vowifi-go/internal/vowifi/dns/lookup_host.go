package dns

import (
	"context"
	"errors"
	"net"
	"os"
	"strings"
	"sync"
	"time"
)

const hostLookupStageTimeout = 5 * time.Second

var (
	publicFallbackDNSServers = []string{"1.1.1.1", "8.8.8.8"}

	hostIPCacheMu sync.Mutex
	hostIPCache   = map[string][]net.IP{}
)

type hostLookupFuncs struct {
	viaServer func(context.Context, string, string) ([]net.IP, error)
	viaSystem func(context.Context, string) ([]net.IP, error)
	public    []string
}

// LookupHostIPStaged tries the configured server, then the system resolver,
// then public resolvers. A previous success is reused only after a timeout.
func LookupHostIPStaged(ctx context.Context, host, configured string) ([]net.IP, error) {
	return lookupHostIPStaged(ctx, host, configured, hostLookupFuncs{
		viaServer: lookupHostViaServer,
		viaSystem: LookupHostIP,
		public:    append([]string(nil), publicFallbackDNSServers...),
	})
}

func lookupHostIPStaged(
	ctx context.Context,
	host, configured string,
	funcs hostLookupFuncs,
) ([]net.IP, error) {
	host = strings.TrimSpace(host)
	if host == "" {
		return nil, errors.New("dns: empty host")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if funcs.viaServer == nil {
		funcs.viaServer = lookupHostViaServer
	}
	if funcs.viaSystem == nil {
		funcs.viaSystem = LookupHostIP
	}

	var firstErr error
	timedOut := false
	try := func(lookup func(context.Context) ([]net.IP, error)) []net.IP {
		stageCtx, cancel := context.WithTimeout(ctx, hostLookupStageTimeout)
		defer cancel()
		ips, err := lookup(stageCtx)
		if err != nil {
			if isLookupTimeout(err) {
				timedOut = true
			}
			if firstErr == nil {
				firstErr = err
			}
			return nil
		}
		if len(ips) == 0 {
			if firstErr == nil {
				firstErr = errors.New("dns: no usable IP addresses")
			}
			return nil
		}
		storeHostIPs(host, ips)
		return ips
	}

	if configured = strings.TrimSpace(configured); configured != "" {
		if _, err := resolverForEndpoint(configured); err != nil {
			return nil, err
		}
		if ips := try(func(stageCtx context.Context) ([]net.IP, error) {
			return funcs.viaServer(stageCtx, host, configured)
		}); len(ips) > 0 {
			return ips, nil
		}
	}

	if ips := try(func(stageCtx context.Context) ([]net.IP, error) {
		return funcs.viaSystem(stageCtx, host)
	}); len(ips) > 0 {
		return ips, nil
	}

	for _, server := range funcs.public {
		if ips := try(func(stageCtx context.Context) ([]net.IP, error) {
			return funcs.viaServer(stageCtx, host, server)
		}); len(ips) > 0 {
			return ips, nil
		}
	}

	if timedOut {
		if cached := cachedHostIPs(host); len(cached) > 0 {
			return cached, nil
		}
	}
	if firstErr == nil {
		firstErr = errors.New("dns: no usable IP addresses")
	}
	return nil, firstErr
}

func lookupHostViaServer(ctx context.Context, host, server string) ([]net.IP, error) {
	resolver, err := resolverForEndpoint(server)
	if err != nil {
		return nil, err
	}
	addresses, err := resolver.LookupIPAddr(ctx, host)
	if err != nil {
		return nil, err
	}
	preferred, fallback := collectResolvedIPs(addresses, false, make(map[string]struct{}), nil, nil)
	ips := append(preferred, fallback...)
	if len(ips) == 0 {
		return nil, errors.New("dns: no usable IP addresses")
	}
	return cloneIPs(ips), nil
}

func isLookupTimeout(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, os.ErrDeadlineExceeded) {
		return true
	}
	var netErr net.Error
	return errors.As(err, &netErr) && netErr.Timeout()
}

func cachedHostIPs(host string) []net.IP {
	hostIPCacheMu.Lock()
	defer hostIPCacheMu.Unlock()
	return cloneIPs(hostIPCache[host])
}

func storeHostIPs(host string, ips []net.IP) {
	if host == "" || len(ips) == 0 {
		return
	}
	hostIPCacheMu.Lock()
	defer hostIPCacheMu.Unlock()
	hostIPCache[host] = cloneIPs(ips)
}

// ClearHostIPCache drops last-success lookup results. Tests use this between cases.
func ClearHostIPCache() {
	hostIPCacheMu.Lock()
	defer hostIPCacheMu.Unlock()
	hostIPCache = map[string][]net.IP{}
}
