//go:build linux

package swu

import (
	"errors"
	"fmt"
	"net"
	"strings"

	"github.com/iniwex5/netlink"
	"github.com/iniwex5/vowifi-go/engine/driver"
)

const defaultXFRMInterface = "ipsec0"

type xfrmDataPlane struct {
	manager         *driver.XFRMManager
	network         *driver.NetTxn
	name            string
	disableUDPEncap func() error
}

func (x *xfrmDataPlane) DeviceName() string { return x.name }

func (x *xfrmDataPlane) EnsureIPv6Enabled() error {
	if x == nil || x.network == nil || x.name == "" {
		return errors.New("swu: XFRM network transaction is not initialized")
	}
	return x.network.EnsureIPv6Enabled(x.name)
}

func (x *xfrmDataPlane) Close() error {
	if x == nil {
		return nil
	}
	var networkErr, managerErr, socketErr error
	if x.network != nil {
		networkErr = x.network.Rollback()
	}
	if x.manager != nil {
		managerErr = x.manager.CleanupChecked()
	}
	if x.disableUDPEncap != nil {
		socketErr = x.disableUDPEncap()
	}
	return errors.Join(networkErr, managerErr, socketErr)
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
	if err := s.socket.SetUDPEncap(true); err != nil {
		return fmt.Errorf("swu: enable UDP encapsulation for XFRM: %w", err)
	}
	transport := s.socket
	plane := &xfrmDataPlane{
		manager: driver.NewXFRMManager(), network: driver.NewNetTools().Begin(),
		disableUDPEncap: func() error { return transport.SetUDPEncap(false) },
	}
	if err := s.installXFRMDataPlane(
		plane, keys, localIP, remoteIP, localPort, remotePort, underlyingIndex,
	); err != nil {
		return errors.Join(err, plane.Close())
	}
	s.kernelDataPlane = plane
	return nil
}

func resolveXFRMLocalRoute(localIP, remoteIP net.IP) (net.IP, int, error) {
	if localIP != nil && !localIP.IsUnspecified() {
		index, err := interfaceIndexForIP(localIP)
		return localIP, index, err
	}
	routes, err := netlink.RouteGet(remoteIP)
	if err != nil {
		return nil, 0, fmt.Errorf("swu: resolve XFRM outbound route: %w", err)
	}
	for _, route := range routes {
		if route.Src != nil && !route.Src.IsUnspecified() {
			if route.LinkIndex <= 0 {
				return nil, 0, errors.New("swu: outbound XFRM route has no interface")
			}
			return route.Src, route.LinkIndex, nil
		}
	}
	return nil, 0, errors.New("swu: outbound route has no source address")
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

func (s *Session) installXFRMDataPlane(
	plane *xfrmDataPlane,
	keys *childSAKeys,
	localIP, remoteIP net.IP,
	localPort, remotePort uint16,
	underlyingIndex int,
) error {
	name, ifID := s.xfrmInterfaceIdentity()
	if ifID == 0 || s.espRemoteSPI == 0 || s.espLocalSPI == 0 {
		return errors.New("swu: XFRM requires non-zero interface ID and ESP SPIs")
	}
	plane.name = name
	if err := plane.manager.AddXFRMInterface(name, ifID, underlyingIndex); err != nil {
		return err
	}
	outbound, inbound, err := s.xfrmSAConfigs(keys, localIP, remoteIP, localPort, remotePort, ifID)
	if err != nil {
		return err
	}
	if err := plane.manager.AddSA(outbound); err != nil {
		return err
	}
	if err := plane.manager.AddSA(inbound); err != nil {
		return err
	}
	if err := installXFRMPolicies(plane.manager, outbound, inbound, ifID); err != nil {
		return err
	}
	if err := s.configureNetworkInterface(plane.network, name); err != nil {
		return fmt.Errorf("swu: configure XFRM interface: %w", err)
	}
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

func (s *Session) xfrmSAConfigs(
	keys *childSAKeys,
	localIP, remoteIP net.IP,
	localPort, remotePort uint16,
	ifID uint32,
) (driver.XFRMSAConfig, driver.XFRMSAConfig, error) {
	outbound := s.baseXFRMSA(localIP, remoteIP, localPort, remotePort, s.espRemoteSPI, ifID, netlink.XFRM_SA_DIR_OUT)
	inbound := s.baseXFRMSA(remoteIP, localIP, remotePort, localPort, s.espLocalSPI, ifID, netlink.XFRM_SA_DIR_IN)
	if err := s.applyXFRMAlgorithms(&outbound, keys.initiator); err != nil {
		return outbound, inbound, err
	}
	if err := s.applyXFRMAlgorithms(&inbound, keys.responder); err != nil {
		return outbound, inbound, err
	}
	return outbound, inbound, nil
}

func (s *Session) baseXFRMSA(
	source, destination net.IP,
	sourcePort, destinationPort uint16,
	spi, ifID uint32,
	direction netlink.SADir,
) driver.XFRMSAConfig {
	return driver.XFRMSAConfig{
		Src: source, Dst: destination, SPI: spi, Proto: netlink.XFRM_PROTO_ESP,
		Mode: netlink.XFRM_MODE_TUNNEL, IsAEAD: driver.IsAEADAlgorithm(s.espCipher),
		EncapType: netlink.XFRM_ENCAP_ESPINUDP, EncapSrcPort: int(sourcePort),
		EncapDstPort: int(destinationPort), Ifid: int(ifID), ReplayWindow: s.cfg.ReplayWindow,
		SADir: direction, ESN: s.cfg.EnableESN,
	}
}

func (s *Session) applyXFRMAlgorithms(config *driver.XFRMSAConfig, keys childDirectionKeys) error {
	if config.IsAEAD {
		algorithm, err := driver.IKEv2AlgToXFRMAead(s.espCipher, int(s.espEncKeyBits))
		if err != nil {
			return err
		}
		config.AeadAlgoName, config.AeadKey, config.AeadICVLen = algorithm.Name, keys.enc, algorithm.ICVBits
		return validateXFRMKeyLength("AEAD", keys.enc, algorithm.KeyBits)
	}
	crypt, err := driver.IKEv2AlgToXFRMCrypt(s.espCipher, int(s.espEncKeyBits))
	if err != nil {
		return err
	}
	auth, err := driver.IKEv2AlgToXFRMAuth(s.espInteg)
	if err != nil {
		return err
	}
	config.CryptAlgoName, config.CryptKey = crypt.Name, keys.enc
	config.AuthAlgoName, config.AuthKey, config.AuthTruncLen = auth.Name, keys.integ, auth.TruncateBits
	return errors.Join(
		validateXFRMKeyLength("encryption", keys.enc, crypt.KeyBits),
		validateXFRMKeyLength("authentication", keys.integ, auth.KeyBits),
	)
}

func validateXFRMKeyLength(kind string, key []byte, expectedBits int) error {
	if len(key)*8 != expectedBits {
		return fmt.Errorf("swu: XFRM %s key has %d bits, want %d", kind, len(key)*8, expectedBits)
	}
	return nil
}

func installXFRMPolicies(
	manager *driver.XFRMManager,
	outbound, inbound driver.XFRMSAConfig,
	ifID uint32,
) error {
	allIPv4 := &net.IPNet{IP: net.IPv4zero, Mask: net.CIDRMask(0, 32)}
	allIPv6 := &net.IPNet{IP: net.IPv6zero, Mask: net.CIDRMask(0, 128)}
	for _, network := range []*net.IPNet{allIPv4, allIPv6} {
		policies := []driver.XFRMSPConfig{
			xfrmPolicy(network, netlink.XFRM_DIR_OUT, outbound, ifID),
			xfrmPolicy(network, netlink.XFRM_DIR_IN, inbound, ifID),
			xfrmPolicy(network, netlink.XFRM_DIR_FWD, inbound, ifID),
		}
		for _, policy := range policies {
			if err := manager.AddSP(policy); err != nil {
				return err
			}
		}
	}
	return nil
}

func xfrmPolicy(network *net.IPNet, direction netlink.Dir, state driver.XFRMSAConfig, ifID uint32) driver.XFRMSPConfig {
	return driver.XFRMSPConfig{
		Src: network, Dst: network, Dir: direction, TmplSrc: state.Src, TmplDst: state.Dst,
		TmplProto: netlink.XFRM_PROTO_ESP, TmplMode: netlink.XFRM_MODE_TUNNEL,
		TmplSPI: int(state.SPI), Ifid: int(ifID),
	}
}
