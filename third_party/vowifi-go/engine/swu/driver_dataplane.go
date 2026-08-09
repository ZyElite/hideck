package swu

import (
	"errors"
	"fmt"
	"strings"

	"github.com/iniwex5/vowifi-go/engine/driver"
	"github.com/iniwex5/vowifi-go/engine/ipsec"
)

const (
	defaultTunnelMTU = 1358
	minimumIPv6MTU   = 1280
	maximumIPPacket  = 65535
)

type kernelDataPlane interface {
	Close() error
	DeviceName() string
	EnsureIPv6Enabled() error
	Rekey(*Session, *childSARuntime) error
}

func normalizeDataplaneMode(mode string) (string, error) {
	normalized := strings.ToLower(strings.TrimSpace(mode))
	if normalized == "" {
		return DataplaneModeUserspace, nil
	}
	switch normalized {
	case DataplaneModeUserspace, DataplaneModeTUN, DataplaneModeXFRMI:
		return normalized, nil
	default:
		return "", fmt.Errorf("swu: unsupported data plane mode %q", mode)
	}
}

func (s *Session) setupTUNDataPlane() error {
	tun, err := s.openTUNDevice(strings.TrimSpace(s.cfg.TUNName))
	if err != nil {
		return err
	}
	s.tun = tun
	if network, ok := s.net.(*driver.NetTools); ok {
		s.networkTxn = network.Begin()
	} else {
		s.legacyNetwork = newLegacyNetTxn(s.net)
	}
	if err := s.applyNetworkConfigOnTUN(tun.DeviceName()); err != nil {
		s.tun = nil
		return errors.Join(err, s.rollbackNetworkConfig(), tun.Close())
	}
	return nil
}

func (s *Session) openTUNDevice(name string) (TUN, error) {
	var (
		tun TUN
		err error
	)
	if s.cfg.TUNFactory != nil {
		tun, err = s.cfg.TUNFactory(name)
	} else {
		tun, err = driver.NewTUNDevice(name)
	}
	if err != nil {
		return nil, err
	}
	if tun == nil {
		return nil, errors.New("swu: TUN factory returned nil")
	}
	return tun, nil
}

func (s *Session) activeDriverInterface() string {
	if s.tun != nil {
		return s.tun.DeviceName()
	}
	if s.kernelDataPlane != nil {
		return s.kernelDataPlane.DeviceName()
	}
	return ""
}

func (s *Session) configureNetworkInterface(transaction *driver.NetTxn, iface string) error {
	if transaction == nil {
		return errors.New("swu: nil network transaction")
	}
	if err := transaction.SetLinkUp(iface); err != nil {
		return err
	}
	if err := transaction.SetMTU(iface, s.tunnelMTU()); err != nil {
		return err
	}
	routes, hasIPv6, err := s.dataPlaneRoutes()
	if err != nil {
		return err
	}
	if s.innerIPv6 != nil || hasIPv6 {
		if err := transaction.EnsureIPv6Enabled(iface); err != nil {
			return err
		}
	}
	if err := s.addInnerAddresses(transaction, iface); err != nil {
		return err
	}
	return s.addPolicyRoutes(transaction, iface, routes)
}

func (s *Session) configureLegacyNetworkInterface(transaction *legacyNetTxn, iface string) error {
	if transaction == nil {
		return errors.New("swu: nil injected network transaction")
	}
	if err := transaction.SetLinkUp(iface); err != nil {
		return err
	}
	if err := transaction.SetMTU(iface, s.tunnelMTU()); err != nil {
		return err
	}
	routes, hasIPv6, err := s.dataPlaneRoutes()
	if err != nil {
		return err
	}
	if s.innerIPv6 != nil || hasIPv6 {
		if err := transaction.EnsureIPv6Enabled(iface); err != nil {
			return err
		}
	}
	if err := s.addLegacyInnerAddresses(transaction, iface); err != nil {
		return err
	}
	return s.addLegacyRoutes(transaction, iface, routes)
}

func (s *Session) addLegacyInnerAddresses(transaction *legacyNetTxn, iface string) error {
	if s.innerIP != nil {
		prefix := validPrefix(s.innerPrefix, 32)
		if err := transaction.AddAddress(iface, fmt.Sprintf("%s/%d", s.innerIP, prefix)); err != nil {
			return err
		}
	}
	if s.innerIPv6 == nil {
		return nil
	}
	prefix := validPrefix(s.innerIPv6Prefix, 128)
	return transaction.AddAddress6(iface, fmt.Sprintf("%s/%d", s.innerIPv6, prefix))
}

func (s *Session) addLegacyRoutes(transaction *legacyNetTxn, iface string, routes []dataPlaneRoute) error {
	for _, route := range routes {
		if route.cidr == "0.0.0.0/0" || route.cidr == "::/0" {
			continue
		}
		if err := transaction.AddRoute(route.cidr, "", iface, route.ipv6); err != nil {
			return err
		}
	}
	return nil
}

func (s *Session) tunnelMTU() int {
	mtu := s.cfg.TUNMTU
	if mtu <= 0 {
		mtu = defaultTunnelMTU
	}
	if s.innerIPv6 != nil && mtu < minimumIPv6MTU {
		return minimumIPv6MTU
	}
	return mtu
}

func (s *Session) addInnerAddresses(transaction *driver.NetTxn, iface string) error {
	if s.innerIP != nil {
		prefix := validPrefix(s.innerPrefix, 32)
		if err := transaction.AddAddress(iface, fmt.Sprintf("%s/%d", s.innerIP, prefix)); err != nil {
			return err
		}
	}
	if s.innerIPv6 != nil {
		prefix := validPrefix(s.innerIPv6Prefix, 128)
		if err := transaction.AddAddress6(iface, fmt.Sprintf("%s/%d", s.innerIPv6, prefix)); err != nil {
			return err
		}
	}
	return nil
}

func validPrefix(prefix, bits int) int {
	if prefix <= 0 || prefix > bits {
		return bits
	}
	return prefix
}

func (s *Session) addPolicyRoutes(transaction *driver.NetTxn, iface string, routes []dataPlaneRoute) error {
	link, err := driver.NewNetTools().GetLink(iface)
	if err != nil {
		return err
	}
	table := link.Attrs().Index + 1000
	if err := transaction.AddInputRule(iface, table); err != nil {
		return err
	}
	for _, source := range s.innerSourceCIDRs() {
		if err := transaction.AddRule(source, table); err != nil {
			return err
		}
	}
	for _, route := range routes {
		if err := transaction.AddRouteTable(route.cidr, iface, table); err != nil {
			return err
		}
	}
	return nil
}

func (s *Session) innerSourceCIDRs() []string {
	var sources []string
	if s.innerIP != nil {
		sources = append(sources, fmt.Sprintf("%s/32", s.innerIP))
	}
	if s.innerIPv6 != nil {
		sources = append(sources, fmt.Sprintf("%s/128", s.innerIPv6))
	}
	return sources
}

func (s *Session) startTUNDataPlaneLoop() {
	transport := s.transport()
	s.dataPlaneWG.Add(2)
	go s.loopESPToTUN(transport, s.tun)
	go s.loopTUNToESP(transport, s.tun)
}

func (s *Session) loopESPToTUN(transport ipsec.Transport, tun TUN) {
	defer s.dataPlaneWG.Done()
	for {
		select {
		case <-s.ctx.Done():
			return
		case raw, ok := <-transport.ESPPackets():
			if !ok {
				replacement := s.replacementTransport(transport)
				if replacement == nil {
					return
				}
				transport = replacement
				continue
			}
			inner, _, err := s.decapsulateOuterESP(raw)
			if err != nil {
				continue
			}
			written, err := tun.Write(inner)
			if err != nil {
				s.failDataPlane(fmt.Errorf("swu: write TUN packet: %w", err))
				return
			}
			if written != len(inner) {
				s.failDataPlane(fmt.Errorf("swu: short TUN write: wrote %d of %d bytes", written, len(inner)))
				return
			}
		}
	}
}

func (s *Session) loopTUNToESP(transport ipsec.Transport, tun TUN) {
	defer s.dataPlaneWG.Done()
	buffer := make([]byte, maximumIPPacket)
	for {
		length, err := tun.Read(buffer)
		if err != nil {
			if s.ctx.Err() == nil {
				s.failDataPlane(fmt.Errorf("swu: read TUN packet: %w", err))
			}
			return
		}
		if length <= 0 {
			continue
		}
		lease, err := s.encapsulateInnerPacketLease(buffer[:length])
		if err != nil {
			continue
		}
		active := s.transport()
		if active == nil {
			lease.Release()
			s.failDataPlane(errors.New("swu: active ESP transport disappeared"))
			return
		}
		err = active.SendESP(lease.data)
		lease.Release()
		if err != nil {
			s.failDataPlane(fmt.Errorf("swu: send ESP packet: %w", err))
			return
		}
	}
}

func (s *Session) failDataPlane(err error) {
	s.setTerminalError(err)
	s.cancel()
}
