//go:build linux

package swu

import (
	"errors"
	"net"

	"github.com/iniwex5/vowifi-go/engine/driver"
	"github.com/iniwex5/vowifi-go/engine/ipsec"
	"github.com/iniwex5/vowifi-go/engine/logger"
	"go.uber.org/zap"
)

func (x *xfrmDataPlane) UpdateOuterAddresses(s *Session, tuple xfrmOuterTuple) error {
	if x == nil || s == nil {
		return errors.New("swu: XFRM MOBIKE requires an active data plane")
	}
	if err := validateXFRMTuple(tuple.localIP, tuple.remoteIP, tuple.localPort, tuple.remotePort); err != nil {
		return err
	}
	direct, ok := tuple.transport.(*ipsec.SocketManager)
	if !ok {
		return errors.New("swu: XFRM MOBIKE requires a direct UDP transport")
	}
	if err := tuple.transport.SetUDPEncap(); err != nil {
		return err
	}
	x.mu.Lock()
	defer x.mu.Unlock()
	if x.manager == nil {
		return errors.Join(
			errors.New("swu: XFRM MOBIKE requires an active data plane"),
			direct.DisableUDPEncap(),
		)
	}
	if x.matchesOuterTuple(tuple) {
		x.disableUDPEncap = direct.DisableUDPEncap
		return nil
	}
	update := x.buildMOBIKEUpdate(tuple)
	if err := x.addMOBIKEStates(update.newStates); err != nil {
		return errors.Join(err, direct.DisableUDPEncap())
	}
	newPolicies := xfrmPolicySet{outbound: update.outbound, inbound: update.inbound, ifID: x.ifID}
	if err := updateXFRMPolicies(x.manager, newPolicies); err != nil {
		oldPolicies := xfrmPolicySet{outbound: x.outbound, inbound: x.inbound, ifID: x.ifID}
		return errors.Join(
			err, updateXFRMPolicies(x.manager, oldPolicies),
			x.deleteMOBIKEStates(update.newStates), direct.DisableUDPEncap(),
		)
	}
	x.removeOldMOBIKEStates(update.oldStates)
	x.localIP, x.remoteIP = cloneIP(tuple.localIP), cloneIP(tuple.remoteIP)
	x.localPort, x.remotePort = tuple.localPort, tuple.remotePort
	x.outbound, x.inbound = update.outbound, update.inbound
	x.retiredInbound = update.retired
	x.disableUDPEncap = direct.DisableUDPEncap
	return nil
}

type xfrmMOBIKEUpdate struct {
	outbound, inbound driver.XFRMSAConfig
	retired           map[uint32]driver.XFRMSAConfig
	newStates         []driver.XFRMSAConfig
	oldStates         []driver.XFRMSAConfig
}

func (x *xfrmDataPlane) buildMOBIKEUpdate(tuple xfrmOuterTuple) xfrmMOBIKEUpdate {
	outbound := migrateXFRMState(x.outbound, tuple.localIP, tuple.remoteIP, tuple.localPort, tuple.remotePort)
	inbound := migrateXFRMState(x.inbound, tuple.remoteIP, tuple.localIP, tuple.remotePort, tuple.localPort)
	update := xfrmMOBIKEUpdate{
		outbound: outbound, inbound: inbound,
		retired:   make(map[uint32]driver.XFRMSAConfig, len(x.retiredInbound)),
		newStates: []driver.XFRMSAConfig{outbound, inbound},
		oldStates: []driver.XFRMSAConfig{x.outbound, x.inbound},
	}
	for spi, state := range x.retiredInbound {
		migrated := migrateXFRMState(state, tuple.remoteIP, tuple.localIP, tuple.remotePort, tuple.localPort)
		update.retired[spi] = migrated
		update.newStates = append(update.newStates, migrated)
		update.oldStates = append(update.oldStates, state)
	}
	return update
}

func migrateXFRMState(
	state driver.XFRMSAConfig,
	source, destination net.IP,
	sourcePort, destinationPort uint16,
) driver.XFRMSAConfig {
	state.Src, state.Dst = cloneIP(source), cloneIP(destination)
	state.EncapSrcPort, state.EncapDstPort = int(sourcePort), int(destinationPort)
	return state
}

func (x *xfrmDataPlane) addMOBIKEStates(states []driver.XFRMSAConfig) error {
	added := make([]driver.XFRMSAConfig, 0, len(states))
	for _, state := range states {
		if err := x.manager.AddSA(state); err != nil {
			return errors.Join(err, x.deleteMOBIKEStates(added))
		}
		added = append(added, state)
	}
	return nil
}

func (x *xfrmDataPlane) deleteMOBIKEStates(states []driver.XFRMSAConfig) error {
	var result error
	for _, state := range states {
		result = errors.Join(result, x.deleteState(state))
	}
	return result
}

func (x *xfrmDataPlane) removeOldMOBIKEStates(states []driver.XFRMSAConfig) {
	for _, state := range states {
		if err := x.deleteState(state); err != nil {
			logger.Warn("XFRM MOBIKE committed but old state could not be removed", zap.Error(err))
		}
	}
}

func (x *xfrmDataPlane) matchesOuterTuple(tuple xfrmOuterTuple) bool {
	return x.localIP.Equal(tuple.localIP) && x.remoteIP.Equal(tuple.remoteIP) &&
		x.localPort == tuple.localPort && x.remotePort == tuple.remotePort
}

func cloneIP(value net.IP) net.IP {
	return append(net.IP(nil), value...)
}
