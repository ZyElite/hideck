//go:build linux

package swu

import (
	"errors"
	"fmt"
	"net"

	"github.com/iniwex5/netlink"
	"github.com/iniwex5/vowifi-go/engine/driver"
	"github.com/iniwex5/vowifi-go/engine/logger"
	"go.uber.org/zap"
)

type xfrmSAConfigSpec struct {
	keys                  *childSAKeys
	localIP, remoteIP     net.IP
	localPort, remotePort uint16
	ifID                  uint32
	localSPI, remoteSPI   uint32
}

type xfrmStateSpec struct {
	source, destination         net.IP
	sourcePort, destinationPort uint16
	spi, ifID                   uint32
	direction                   netlink.SADir
}

type xfrmPolicySet struct {
	outbound, inbound driver.XFRMSAConfig
	ifID              uint32
}

type xfrmPolicySpec struct {
	network   *net.IPNet
	direction netlink.Dir
	state     driver.XFRMSAConfig
	ifID      uint32
	bindSPI   bool
}

func (s *Session) xfrmSAConfigsFor(spec xfrmSAConfigSpec) (driver.XFRMSAConfig, driver.XFRMSAConfig, error) {
	if spec.keys == nil {
		return driver.XFRMSAConfig{}, driver.XFRMSAConfig{}, errors.New("swu: nil XFRM CHILD_SA keys")
	}
	outbound := s.baseXFRMSA(xfrmStateSpec{
		source: spec.localIP, destination: spec.remoteIP,
		sourcePort: spec.localPort, destinationPort: spec.remotePort,
		spi: spec.remoteSPI, ifID: spec.ifID, direction: netlink.XFRM_SA_DIR_OUT,
	})
	inbound := s.baseXFRMSA(xfrmStateSpec{
		source: spec.remoteIP, destination: spec.localIP,
		sourcePort: spec.remotePort, destinationPort: spec.localPort,
		spi: spec.localSPI, ifID: spec.ifID, direction: netlink.XFRM_SA_DIR_IN,
	})
	if err := s.applyXFRMAlgorithms(&outbound, spec.keys.initiator); err != nil {
		return outbound, inbound, err
	}
	if err := s.applyXFRMAlgorithms(&inbound, spec.keys.responder); err != nil {
		return outbound, inbound, err
	}
	return outbound, inbound, nil
}

func (s *Session) baseXFRMSA(spec xfrmStateSpec) driver.XFRMSAConfig {
	return driver.XFRMSAConfig{
		Src: spec.source, Dst: spec.destination, SPI: spec.spi, Proto: netlink.XFRM_PROTO_ESP,
		Mode: netlink.XFRM_MODE_TUNNEL, IsAEAD: driver.IsAEADAlgorithm(s.espCipher),
		EncapType: netlink.XFRM_ENCAP_ESPINUDP, EncapSrcPort: int(spec.sourcePort),
		EncapDstPort: int(spec.destinationPort), Ifid: int(spec.ifID), ReplayWindow: s.cfg.ReplayWindow,
		SADir: spec.direction, ESN: s.espESN,
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
	manager xfrmManager,
	set xfrmPolicySet,
) error {
	for index, policy := range xfrmPolicies(set.outbound, set.inbound, set.ifID) {
		if err := manager.AddSP(policy); err != nil {
			if index < 2 {
				return err
			}
			logger.Warn("optional XFRM policy installation failed",
				zap.Int("index", index), zap.Error(err))
		}
	}
	return nil
}

func updateXFRMPolicies(
	manager xfrmManager,
	set xfrmPolicySet,
) error {
	for index, policy := range xfrmPolicies(set.outbound, set.inbound, set.ifID) {
		if err := manager.UpdateSP(policy); err != nil {
			if index < 2 {
				return err
			}
			logger.Warn("optional XFRM policy update failed",
				zap.Int("index", index), zap.Error(err))
		}
	}
	return nil
}

func xfrmPolicies(outbound, inbound driver.XFRMSAConfig, ifID uint32) []driver.XFRMSPConfig {
	allIPv4 := &net.IPNet{IP: net.IPv4zero, Mask: net.CIDRMask(0, 32)}
	allIPv6 := &net.IPNet{IP: net.IPv6zero, Mask: net.CIDRMask(0, 128)}
	return []driver.XFRMSPConfig{
		xfrmPolicy(xfrmPolicySpec{network: allIPv4, direction: netlink.XFRM_DIR_OUT, state: outbound, ifID: ifID, bindSPI: true}),
		xfrmPolicy(xfrmPolicySpec{network: allIPv4, direction: netlink.XFRM_DIR_IN, state: inbound, ifID: ifID, bindSPI: true}),
		xfrmPolicy(xfrmPolicySpec{network: allIPv4, direction: netlink.XFRM_DIR_FWD, state: inbound, ifID: ifID}),
		xfrmPolicy(xfrmPolicySpec{network: allIPv6, direction: netlink.XFRM_DIR_OUT, state: outbound, ifID: ifID, bindSPI: true}),
		xfrmPolicy(xfrmPolicySpec{network: allIPv6, direction: netlink.XFRM_DIR_IN, state: inbound, ifID: ifID, bindSPI: true}),
	}
}

func xfrmPolicy(spec xfrmPolicySpec) driver.XFRMSPConfig {
	policy := driver.XFRMSPConfig{
		Src: spec.network, Dst: spec.network, Dir: spec.direction,
		TmplSrc: spec.state.Src, TmplDst: spec.state.Dst,
		TmplProto: netlink.XFRM_PROTO_ESP, TmplMode: netlink.XFRM_MODE_TUNNEL,
		Ifid: int(spec.ifID),
	}
	if spec.bindSPI {
		policy.TmplSPI = int(spec.state.SPI)
	}
	return policy
}
