//go:build linux

package swu

import (
	"errors"
	"fmt"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/iniwex5/netlink"
	"github.com/iniwex5/vowifi-go/engine/driver"
	"github.com/iniwex5/vowifi-go/engine/ipsec"
)

const defaultXFRMInterface = "ipsec0"

type xfrmDataPlane struct {
	mu              sync.Mutex
	closeOnce       sync.Once
	closeErr        error
	manager         xfrmManager
	configure       func(string) error
	enableIPv6      func(string) error
	rollbackNetwork func() error
	name            string
	disableUDPEncap func() error
	localIP         net.IP
	remoteIP        net.IP
	localPort       uint16
	remotePort      uint16
	ifID            uint32
	outbound        driver.XFRMSAConfig
	inbound         driver.XFRMSAConfig
	retiredInbound  map[uint32]driver.XFRMSAConfig
	monitorMu       sync.Mutex
	monitorCancel   func()
	monitorWG       sync.WaitGroup
	monitorOpen     xfrmMonitorOpen
	monitorClosed   bool
}

type xfrmInstallSpec struct {
	plane                 *xfrmDataPlane
	keys                  *childSAKeys
	localIP, remoteIP     net.IP
	localPort, remotePort uint16
	underlyingIndex       int
}

func (x *xfrmDataPlane) DeviceName() string { return x.name }

func (x *xfrmDataPlane) CurrentSPIs() (uint32, uint32) {
	if x == nil {
		return 0, 0
	}
	x.mu.Lock()
	defer x.mu.Unlock()
	return x.outbound.SPI, x.inbound.SPI
}

func (x *xfrmDataPlane) LastOutboundUse() (time.Time, error) {
	if x == nil {
		return time.Time{}, nil
	}
	x.mu.Lock()
	manager, spi := x.manager, x.outbound.SPI
	localIP, remoteIP := append(net.IP(nil), x.localIP...), append(net.IP(nil), x.remoteIP...)
	x.mu.Unlock()
	reader, ok := manager.(interface {
		GetSALastUsed(uint32, net.IP, net.IP, netlink.Proto) (uint64, error)
	})
	if !ok || spi == 0 {
		return time.Time{}, nil
	}
	used, err := reader.GetSALastUsed(spi, localIP, remoteIP, netlink.XFRM_PROTO_ESP)
	if err != nil || used == 0 {
		return time.Time{}, err
	}
	return time.Unix(int64(used), 0), nil
}

func (x *xfrmDataPlane) EnsureIPv6Enabled() error {
	if x == nil {
		return errors.New("swu: XFRM network transaction is not initialized")
	}
	x.mu.Lock()
	defer x.mu.Unlock()
	if x.manager == nil || x.enableIPv6 == nil || x.name == "" {
		return errors.New("swu: XFRM network transaction is not initialized")
	}
	return x.enableIPv6(x.name)
}

func (x *xfrmDataPlane) Close() error {
	if x == nil {
		return nil
	}
	x.closeOnce.Do(func() {
		x.stopExpireMonitor()
		x.mu.Lock()
		defer x.mu.Unlock()
		var networkErr, managerErr, socketErr error
		if x.rollbackNetwork != nil {
			networkErr = x.rollbackNetwork()
			x.rollbackNetwork = nil
		}
		if x.manager != nil {
			managerErr = x.manager.CleanupChecked()
			x.manager = nil
		}
		if x.disableUDPEncap != nil {
			socketErr = x.disableUDPEncap()
			x.disableUDPEncap = nil
		}
		x.outbound = driver.XFRMSAConfig{}
		x.inbound = driver.XFRMSAConfig{}
		clear(x.retiredInbound)
		x.closeErr = errors.Join(networkErr, managerErr, socketErr)
	})
	return x.closeErr
}

func (s *Session) setupKernelXFRMDataPlane(keys *childSAKeys) error {
	if keys == nil {
		return errors.New("swu: no child SA keys for XFRM")
	}
	localIP, remoteIP, localPort, remotePort, err := s.resolveXFRMOuterTuple()
	if err != nil {
		return err
	}
	if err := validateXFRMRemoteTuple(remoteIP, localPort, remotePort); err != nil {
		return err
	}
	localIP, underlyingIndex, err := resolveXFRMLocalRoute(localIP, remoteIP)
	if err != nil {
		return err
	}
	if err := validateXFRMTuple(localIP, remoteIP, localPort, remotePort); err != nil {
		return err
	}
	activeTransport := s.transport()
	transport, ok := activeTransport.(*ipsec.SocketManager)
	if !ok {
		return errors.New("swu: XFRM requires a direct UDP transport")
	}
	if err := activeTransport.SetUDPEncap(); err != nil {
		return fmt.Errorf("swu: enable UDP encapsulation for XFRM: %w", err)
	}
	managerFactory := s.xfrmManagerNew
	if managerFactory == nil {
		managerFactory = func() xfrmManager { return driver.NewXFRMManager() }
	}
	plane := &xfrmDataPlane{
		manager:         managerFactory(),
		disableUDPEncap: transport.DisableUDPEncap,
		retiredInbound:  make(map[uint32]driver.XFRMSAConfig),
	}
	if plane.manager == nil {
		return errors.Join(
			errors.New("swu: XFRM manager factory returned nil"),
			plane.disableUDPEncap(),
		)
	}
	s.configureXFRMNetworkPlane(plane)
	if err := s.installXFRMDataPlane(xfrmInstallSpec{
		plane: plane, keys: keys, localIP: localIP, remoteIP: remoteIP,
		localPort: localPort, remotePort: remotePort, underlyingIndex: underlyingIndex,
	}); err != nil {
		return errors.Join(err, plane.Close())
	}
	s.swapKernelDataPlane(plane)
	return nil
}

func (s *Session) configureXFRMNetworkPlane(plane *xfrmDataPlane) {
	if network, ok := s.net.(*driver.NetTools); ok {
		transaction := network.Begin()
		plane.configure = func(iface string) error {
			return s.configureNetworkInterface(transaction, iface)
		}
		plane.enableIPv6 = transaction.EnsureIPv6Enabled
		plane.rollbackNetwork = transaction.Rollback
		return
	}
	transaction := newLegacyNetTxn(s.net)
	plane.configure = func(iface string) error {
		return s.configureLegacyNetworkInterface(transaction, iface)
	}
	plane.enableIPv6 = transaction.EnsureIPv6Enabled
	plane.rollbackNetwork = transaction.Rollback
}

func resolveXFRMLocalRoute(localIP, remoteIP net.IP) (net.IP, int, error) {
	if localIP != nil && !localIP.IsUnspecified() {
		index, err := interfaceIndexForIP(localIP)
		return localIP, index, err
	}
	routedIP, index, _, err := detectOutboundRoute(remoteIP)
	if err != nil {
		return nil, 0, fmt.Errorf("swu: resolve XFRM outbound route: %w", err)
	}
	if index <= 0 {
		return nil, 0, errors.New("swu: outbound XFRM route has no interface")
	}
	return routedIP, index, nil
}

func validateXFRMRemoteTuple(remoteIP net.IP, localPort, remotePort uint16) error {
	if remoteIP == nil || remoteIP.IsUnspecified() {
		return errors.New("swu: XFRM requires a resolved remote outer address")
	}
	if localPort == 0 || remotePort == 0 {
		return fmt.Errorf("swu: XFRM requires non-zero UDP ports, got %d/%d", localPort, remotePort)
	}
	return nil
}

func validateXFRMTuple(localIP, remoteIP net.IP, localPort, remotePort uint16) error {
	if localIP == nil || localIP.IsUnspecified() {
		return errors.New("swu: XFRM requires a resolved local outer address")
	}
	if remoteIP == nil || remoteIP.IsUnspecified() {
		return errors.New("swu: XFRM requires a resolved remote outer address")
	}
	if localPort == 0 || remotePort == 0 {
		return fmt.Errorf("swu: XFRM requires non-zero UDP ports, got %d/%d", localPort, remotePort)
	}
	if (localIP.To4() == nil) != (remoteIP.To4() == nil) {
		return errors.New("swu: XFRM outer address families do not match")
	}
	return nil
}

func (s *Session) installXFRMDataPlane(spec xfrmInstallSpec) error {
	name, ifID := s.xfrmInterfaceIdentity()
	if ifID == 0 || s.espRemoteSPI == 0 || s.espLocalSPI == 0 {
		return errors.New("swu: XFRM requires non-zero interface ID and ESP SPIs")
	}
	spec.plane.name = name
	if err := spec.plane.manager.AddXFRMInterface(name, ifID, spec.underlyingIndex); err != nil {
		return err
	}
	outbound, inbound, err := s.xfrmSAConfigsFor(xfrmSAConfigSpec{
		keys: spec.keys, localIP: spec.localIP, remoteIP: spec.remoteIP,
		localPort: spec.localPort, remotePort: spec.remotePort, ifID: ifID,
		localSPI: s.espLocalSPI, remoteSPI: s.espRemoteSPI,
	})
	if err != nil {
		return err
	}
	if err := spec.plane.manager.AddSA(outbound); err != nil {
		return err
	}
	if err := spec.plane.manager.AddSA(inbound); err != nil {
		return err
	}
	policySet := xfrmPolicySet{outbound: outbound, inbound: inbound, ifID: ifID}
	if err := installXFRMPolicies(spec.plane.manager, policySet); err != nil {
		return err
	}
	if spec.plane.configure == nil {
		return errors.New("swu: XFRM network configuration is not initialized")
	}
	if err := spec.plane.configure(name); err != nil {
		return fmt.Errorf("swu: configure XFRM interface: %w", err)
	}
	spec.plane.localIP = append(net.IP(nil), spec.localIP...)
	spec.plane.remoteIP = append(net.IP(nil), spec.remoteIP...)
	spec.plane.localPort, spec.plane.remotePort = spec.localPort, spec.remotePort
	spec.plane.ifID = ifID
	spec.plane.outbound, spec.plane.inbound = outbound, inbound
	return nil
}

func (s *Session) xfrmInterfaceIdentity() (string, uint32) {
	name := strings.TrimSpace(s.cfg.TUNName)
	if name == "" {
		name = defaultXFRMInterface
	}
	ifID := s.cfg.XFRMIfID
	if ifID == 0 {
		ifID = s.espRemoteSPI
	}
	if ifID == 0 {
		ifID = s.espLocalSPI
	}
	return name, ifID
}

func interfaceIndexForIP(ip net.IP) (int, error) {
	interfaces, err := net.Interfaces()
	if err != nil {
		return 0, fmt.Errorf("swu: list interfaces for XFRM source %s: %w", ip, err)
	}
	var addressErrors error
	for _, iface := range interfaces {
		addresses, err := iface.Addrs()
		if err != nil {
			addressErrors = errors.Join(addressErrors, fmt.Errorf("%s: %w", iface.Name, err))
			continue
		}
		for _, address := range addresses {
			if network, ok := address.(*net.IPNet); ok && network.IP.Equal(ip) {
				return iface.Index, nil
			}
		}
	}
	if addressErrors != nil {
		return 0, fmt.Errorf("swu: inspect interfaces for XFRM source %s: %w", ip, addressErrors)
	}
	return 0, fmt.Errorf("swu: no interface owns XFRM source address %s", ip)
}
