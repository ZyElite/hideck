package netstack

import (
	"context"
	"errors"
	"net"
	"sync"

	"github.com/iniwex5/vowifi-go/engine/swu"
	"github.com/iniwex5/vowifi-go/internal/vowifi/imscore"
	"github.com/iniwex5/vowifi-go/internal/vowifi/ipsec3gpp"
)

// IMSNetworkAdapter exposes the additive IMS interface without weakening the
// original Network method signatures.
type IMSNetworkAdapter struct {
	network      *Network
	ipsecMu      sync.Mutex
	ipsecCleanup func() error
}

var _ imscore.IMSNetwork = (*IMSNetworkAdapter)(nil)

// AdaptIMSNetwork exposes an existing restored Network through the IMS API.
func AdaptIMSNetwork(network *Network) *IMSNetworkAdapter {
	if network == nil {
		return nil
	}
	return &IMSNetworkAdapter{network: network}
}

func NewTunnelNetwork(
	innerIP net.IP,
	prefixLen int,
	dns []string,
	packetIO PacketIO,
) (*IMSNetworkAdapter, error) {
	if packetIO == nil {
		return nil, errors.New("netstack: SWu packet IO is required")
	}
	if normalizeIPv4(innerIP) == nil && normalizeIPv6(innerIP) == nil {
		return nil, errors.New("netstack: negotiated IP address is required")
	}
	dnsServers := make([]net.IP, 0, len(dns))
	for _, server := range dns {
		if ip := net.ParseIP(server); ip != nil {
			dnsServers = append(dnsServers, ip)
		}
	}
	var ipv4Address, ipv6Address net.IP
	if innerIP.To4() != nil {
		ipv4Address = innerIP
	} else {
		ipv6Address = innerIP
	}
	endpoint := packetIOEndpoint{PacketIO: packetIO}
	network, err := NewNetwork(
		context.Background(), ipv4Address, ipv6Address, prefixLen,
		defaultMTU, endpoint, nil, dnsServers,
	)
	if err != nil {
		return nil, err
	}
	return &IMSNetworkAdapter{network: network}, nil
}

func (a *IMSNetworkAdapter) Network() *Network { return a.network }
func (a *IMSNetworkAdapter) LocalIP() net.IP   { return a.network.LocalIP() }

func (a *IMSNetworkAdapter) HasLocalIP(ip net.IP) bool {
	return a.network.HasLocalIP(ip)
}

func (a *IMSNetworkAdapter) ResolveIP(ctx context.Context, host string) (net.IP, error) {
	preferIPv6 := a.network.LocalIP().To4() == nil
	return a.network.ResolveIP(ctx, host, preferIPv6)
}

func (a *IMSNetworkAdapter) LookupSRV(
	ctx context.Context,
	service string,
	proto string,
	name string,
) (string, uint16, error) {
	return a.network.LookupSRV(ctx, service, proto, name)
}

func (a *IMSNetworkAdapter) DialContext(ctx context.Context, network, remote string) (net.Conn, error) {
	return a.network.DialContext(ctx, network, nil, remote, imscore.DialOptions{TCPMSS: imsTCPMSS})
}

func (a *IMSNetworkAdapter) DialTCPContext(
	ctx context.Context,
	local *net.TCPAddr,
	remote *net.TCPAddr,
) (net.Conn, error) {
	return a.network.DialContext(ctx, "tcp", local, remote.String(), imscore.DialOptions{TCPMSS: imsTCPMSS})
}

func (a *IMSNetworkAdapter) ListenTCP(addr *net.TCPAddr) (net.Listener, error) {
	full, protocol, err := a.network.fullAddressFromListenAddr(addr)
	if err != nil {
		return nil, err
	}
	return listenTCPWithMSS(a.network.stack, full, protocol)
}

func (a *IMSNetworkAdapter) ListenPacket(network string, addr *net.UDPAddr) (net.PacketConn, error) {
	return a.network.ListenPacket(context.Background(), network, addr)
}

func (a *IMSNetworkAdapter) InstallIPSec3GPP(policy ipsec3gpp.Policy) error {
	a.ipsecMu.Lock()
	defer a.ipsecMu.Unlock()
	if a.ipsecCleanup != nil {
		if err := a.ipsecCleanup(); err != nil {
			return err
		}
		a.ipsecCleanup = nil
	}
	cleanup, err := a.network.InstallIPSec3GPP(context.Background(), policy)
	if err != nil {
		return err
	}
	a.ipsecCleanup = cleanup
	return nil
}

func (a *IMSNetworkAdapter) RemoveIPSec3GPP() error {
	a.ipsecMu.Lock()
	cleanup := a.ipsecCleanup
	a.ipsecCleanup = nil
	a.ipsecMu.Unlock()
	if cleanup == nil {
		return nil
	}
	return cleanup()
}

func (a *IMSNetworkAdapter) IPSec3GPPPolicyInstalled() bool {
	return a.network.IPSec3GPPPolicyInstalled()
}

func (a *IMSNetworkAdapter) Stats() Stats { return a.network.Stats() }

func (a *IMSNetworkAdapter) Close() error {
	if a == nil || a.network == nil {
		return nil
	}
	return errors.Join(a.RemoveIPSec3GPP(), a.network.Close())
}

type packetIOEndpoint struct {
	PacketIO
}

func (e packetIOEndpoint) ReadPacket(ctx context.Context) ([]byte, error) {
	return e.ReadPacketContext(ctx)
}

func (e packetIOEndpoint) WritePacket(ctx context.Context, packet []byte) error {
	return e.WritePacketContext(ctx, packet)
}

func (e packetIOEndpoint) Snapshot() swu.InnerPacketSnapshot {
	if endpoint, ok := e.PacketIO.(interface {
		Snapshot() swu.InnerPacketSnapshot
	}); ok {
		return endpoint.Snapshot()
	}
	return swu.InnerPacketSnapshot{}
}

func (e packetIOEndpoint) Close() error {
	if closer, ok := e.PacketIO.(interface{ Close() error }); ok {
		return closer.Close()
	}
	return nil
}
