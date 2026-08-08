//go:build linux

package swu

import (
	"errors"

	"github.com/iniwex5/vowifi-go/engine/driver"
	"github.com/iniwex5/vowifi-go/engine/logger"
	"go.uber.org/zap"
)

func (x *xfrmDataPlane) Rekey(s *Session, runtime *childSARuntime) error {
	if x == nil || x.manager == nil || runtime == nil {
		return errors.New("swu: XFRM rekey requires an active data plane and CHILD_SA")
	}
	if runtime.localSPI == 0 || runtime.remoteSPI == 0 {
		return errors.New("swu: XFRM rekey requires non-zero ESP SPIs")
	}
	x.mu.Lock()
	defer x.mu.Unlock()
	keys := &childSAKeys{initiator: runtime.outboundKeys, responder: runtime.inboundKeys}
	outbound, inbound, err := s.xfrmSAConfigsFor(xfrmSAConfigSpec{
		keys: keys, localIP: x.localIP, remoteIP: x.remoteIP,
		localPort: x.localPort, remotePort: x.remotePort, ifID: x.ifID,
		localSPI: runtime.localSPI, remoteSPI: runtime.remoteSPI,
	})
	if err != nil {
		return err
	}
	if err := x.addRekeyStates(outbound, inbound); err != nil {
		return err
	}
	newPolicies := xfrmPolicySet{outbound: outbound, inbound: inbound, ifID: x.ifID}
	if err := updateXFRMPolicies(x.manager, newPolicies); err != nil {
		oldPolicies := xfrmPolicySet{outbound: x.outbound, inbound: x.inbound, ifID: x.ifID}
		rollbackErr := updateXFRMPolicies(x.manager, oldPolicies)
		cleanupErr := x.deleteStates(outbound, inbound)
		return errors.Join(err, rollbackErr, cleanupErr)
	}
	if err := x.deleteState(x.outbound); err != nil {
		logger.Warn("XFRM rekey committed but old outbound state could not be removed", zap.Error(err))
	}
	if x.retiredInbound == nil {
		x.retiredInbound = make(map[uint32]driver.XFRMSAConfig)
	}
	x.retiredInbound[x.inbound.SPI] = x.inbound
	x.outbound, x.inbound = outbound, inbound
	return nil
}

func (x *xfrmDataPlane) RetireInbound(spi uint32) error {
	if x == nil || x.manager == nil {
		return errors.New("swu: XFRM data plane is not active")
	}
	x.mu.Lock()
	defer x.mu.Unlock()
	config, ok := x.retiredInbound[spi]
	if !ok {
		return nil
	}
	if err := x.deleteState(config); err != nil {
		return err
	}
	delete(x.retiredInbound, spi)
	return nil
}

func (x *xfrmDataPlane) addRekeyStates(outbound, inbound driver.XFRMSAConfig) error {
	if err := x.manager.AddSA(outbound); err != nil {
		return err
	}
	if err := x.manager.AddSA(inbound); err != nil {
		return errors.Join(err, x.deleteState(outbound))
	}
	return nil
}

func (x *xfrmDataPlane) deleteStates(outbound, inbound driver.XFRMSAConfig) error {
	return errors.Join(x.deleteState(outbound), x.deleteState(inbound))
}

func (x *xfrmDataPlane) deleteState(config driver.XFRMSAConfig) error {
	return x.manager.DelSA(config.SPI, config.Src, config.Dst, config.Proto)
}
