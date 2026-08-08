package swu

import (
	"errors"
	"fmt"
	"net"

	"github.com/iniwex5/vowifi-go/engine/driver"
	"github.com/iniwex5/vowifi-go/engine/ipsec"
)

type xfrmOuterTuple struct {
	transport             ipsec.Transport
	localIP, remoteIP     net.IP
	localPort, remotePort uint16
}

type kernelMOBIKEUpdater interface {
	UpdateOuterAddresses(*Session, xfrmOuterTuple) error
}

func (s *Session) updateKernelMOBIKETransport(transport ipsec.Transport) error {
	if s.kernelDataPlane == nil {
		return nil
	}
	updater, ok := s.kernelDataPlane.(kernelMOBIKEUpdater)
	if !ok {
		return errors.New("swu: active kernel data plane does not support MOBIKE")
	}
	tuple, err := xfrmTupleForTransport(transport)
	if err != nil {
		return err
	}
	return updater.UpdateOuterAddresses(s, tuple)
}

// updateXFRMState restores the original address-taking XFRM update boundary.
func (s *Session) updateXFRMState(localAddr, remoteAddr string) error {
	if s.kernelDataPlane == nil {
		return nil
	}
	transport := s.transport()
	if transport == nil {
		return errors.New("swu: no active transport for XFRM MOBIKE update")
	}
	tuple, err := xfrmTupleForTransport(transport)
	if err != nil {
		return err
	}
	if tuple.localIP == nil {
		tuple.localIP = net.ParseIP(localAddr)
	}
	if tuple.remoteIP == nil {
		tuple.remoteIP = net.ParseIP(remoteAddr)
	}
	if tuple.localIP == nil || tuple.remoteIP == nil {
		return fmt.Errorf("swu: invalid XFRM MOBIKE addresses local=%q remote=%q", localAddr, remoteAddr)
	}
	updater, ok := s.kernelDataPlane.(kernelMOBIKEUpdater)
	if !ok {
		return errors.New("swu: active kernel data plane does not support MOBIKE")
	}
	return updater.UpdateOuterAddresses(s, tuple)
}

func xfrmTupleForTransport(transport ipsec.Transport) (xfrmOuterTuple, error) {
	if transport == nil {
		return xfrmOuterTuple{}, errors.New("swu: nil transport for XFRM MOBIKE update")
	}
	remotePort := transport.RemotePort()
	if remotePort <= 0 || remotePort > int(^uint16(0)) || transport.LocalPort() == 0 {
		return xfrmOuterTuple{}, fmt.Errorf(
			"swu: invalid XFRM MOBIKE UDP ports %d/%d", transport.LocalPort(), remotePort,
		)
	}
	return xfrmOuterTuple{
		transport: transport,
		localIP:   append(net.IP(nil), transport.LocalIP()...), remoteIP: append(net.IP(nil), transport.RemoteIP()...),
		localPort: transport.LocalPort(), remotePort: uint16(remotePort),
	}, nil
}

// fillSAKeys restores the original XFRM key mapping helper.
func (s *Session) fillSAKeys(config *driver.XFRMSAConfig, sa *ipsec.SecurityAssociation, isAEAD bool) error {
	if config == nil || sa == nil {
		return errors.New("swu: MOBIKE XFRM key mapping requires a config and SA")
	}
	config.IsAEAD = isAEAD
	if isAEAD {
		algorithm, err := driver.IKEv2AlgToXFRMAead(s.espCipher, int(s.espEncKeyBits))
		if err != nil {
			return mobikeAlgorithmError("AEAD", err)
		}
		config.AeadAlgoName = algorithm.Name
		config.AeadKey = sa.EncryptionKey
		config.AeadICVLen = algorithm.ICVBits
		return nil
	}
	crypt, err := driver.IKEv2AlgToXFRMCrypt(s.espCipher, int(s.espEncKeyBits))
	if err != nil {
		return mobikeAlgorithmError("encryption", err)
	}
	auth, err := driver.IKEv2AlgToXFRMAuth(s.espInteg)
	if err != nil {
		return mobikeAlgorithmError("integrity", err)
	}
	config.CryptAlgoName, config.CryptKey = crypt.Name, sa.EncryptionKey
	config.AuthAlgoName, config.AuthKey = auth.Name, sa.IntegrityKey
	config.AuthTruncLen = auth.TruncateBits
	return nil
}

func mobikeAlgorithmError(kind string, err error) error {
	return &NegotiationError{
		Class: ErrClassDriverUnsupported, Reason: fmt.Sprintf("MOBIKE map %s algorithm: %v", kind, err),
	}
}
