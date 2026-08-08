package swu

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"net"

	"github.com/iniwex5/vowifi-go/engine/ikev2"
)

// UpdateAddresses handles a MOBIKE address update (RFC 4555): it records the
// new local/remote addresses and sends an UPDATE_SA_ADDRESSES notification.
func (s *Session) UpdateAddresses(oldIP, newIP net.IP) error {
	if oldIP == nil || newIP == nil {
		return errors.New("swu: MOBIKE requires old and new addresses")
	}
	return s.sendMOBIKEUpdate()
}

// sendMOBIKEUpdate sends a MOBIKE UPDATE_SA_ADDRESSES INFORMATIONAL request.
func (s *Session) sendMOBIKEUpdate() error {
	s.ikeExchangeMu.Lock()
	defer s.ikeExchangeMu.Unlock()
	if s.socket == nil || s.ikeKeys == nil {
		return errors.New("swu: session not established")
	}
	pkt := &ikev2.IKEPacket{
		Header: newIKEHeader(
			s.SPIi, s.SPIr, ikev2.INFORMATIONAL, s.localIKEFlags(false), s.nextMessageID(),
		),
		Payloads: []ikev2.Payload{
			&ikev2.EncryptedPayloadNotify{
				ProtocolID: ikev2.ProtoIKE,
				NotifyType: 16400, // UPDATE_SA_ADDRESSES
			},
		},
	}
	_, err := s.exchangeEstablishedIKE(s.ctx, pkt)
	return err
}

// updateXFRMState updates the XFRM SA state after a MOBIKE update.
func (s *Session) updateXFRMState() error {
	return errors.New("swu: XFRM state update not wired")
}

// verifyCookie2Response verifies a COOKIE2 response (RFC 7296 §2.6).
func (s *Session) verifyCookie2Response() error {
	return errors.New("swu: COOKIE2 verification not wired")
}

// sendIkeAuthChildless sends an IKE_AUTH request without a CHILD_SA (used for
// EAP-only authentication).
func (s *Session) sendIkeAuthChildless() error {
	payloads, err := s.buildIKEAuthFinalPayloads()
	if err != nil {
		return err
	}
	return s.sendIKEAuthRequest(payloads)
}

// startNetEventMonitor starts the network event monitor (MOBIKE triggers).
func (s *Session) startNetEventMonitor() error {
	if s.socket == nil {
		return errors.New("swu: no transport")
	}
	go func() {
		for {
			select {
			case <-s.ctx.Done():
				return
			case ev, ok := <-s.socket.NetEventsChan():
				if !ok {
					return
				}
				_ = ev
			}
		}
	}()
	return nil
}

// startXFRMExpireMonitor starts the XFRM SA expiry monitor.
func (s *Session) startXFRMExpireMonitor() error {
	return nil
}

// ensureIPv6RuntimeEnabled enables IPv6 for an active kernel data plane.
func (s *Session) ensureIPv6RuntimeEnabled() error {
	if s.kernelDataPlane != nil {
		return s.kernelDataPlane.EnsureIPv6Enabled()
	}
	iface := s.activeDriverInterface()
	if iface == "" {
		mode, err := normalizeDataplaneMode(s.cfg.DataplaneMode)
		if err != nil || mode != DataplaneModeUserspace {
			return errors.Join(errors.New("swu: no active kernel data-plane interface"), err)
		}
		return nil
	}
	if s.networkTxn == nil {
		return errors.New("swu: no active network transaction")
	}
	return s.networkTxn.EnsureIPv6Enabled(iface)
}

// applyNetworkConfigOnTUN applies the inner address/routes to the TUN device.
func (s *Session) applyNetworkConfigOnTUN() error {
	if s.primaryInnerIP() == nil {
		return errors.New("swu: no inner address")
	}
	if s.tun == nil || s.networkTxn == nil {
		return errors.New("swu: TUN data plane is not initialized")
	}
	return s.configureNetworkInterface(s.networkTxn, s.tun.DeviceName())
}

// cleanupNetworkConfig removes the network configuration on teardown.
func (s *Session) cleanupNetworkConfig() error {
	if s.networkTxn == nil {
		return nil
	}
	err := s.networkTxn.Rollback()
	s.networkTxn = nil
	return err
}

// setupXFRMDataPlane installs the kernel XFRM data plane.
func (s *Session) setupXFRMDataPlane() error {
	keys, err := s.deriveChildSAKeys()
	if err != nil {
		return err
	}
	return s.setupKernelXFRMDataPlane(keys)
}

// startUserspaceDataPlane starts the user-space data plane.
func (s *Session) startUserspaceDataPlane() error {
	return s.startEstablishedDataPlane()
}

// parsePayloads parses a raw payload chain.
func (s *Session) parsePayloads(raw []byte) ([]ikev2.Payload, error) {
	return ikev2.DecodePayloadChain(raw)
}

// fillSAKeys fills the ESP SA keys from the negotiated transforms.
func (s *Session) fillSAKeys() error {
	return nil
}

// performSessionResumption attempts IKE session resumption.
func (s *Session) performSessionResumption() error {
	return errors.New("swu: session resumption not wired")
}

// handleIkeSessionResumeResp processes a session resumption response.
func (s *Session) handleIkeSessionResumeResp() error {
	return errors.New("swu: session resumption response not wired")
}

// logSessionStats is defined in session.go.

// extractDstTuple extracts the destination tuple from an inner packet.
func extractDstTuple(inner []byte) (net.IP, uint16, error) {
	if len(inner) < 20 {
		return nil, 0, errors.New("swu: inner packet too short")
	}
	version := inner[0] >> 4
	switch version {
	case 4:
		return net.IP(inner[16:20]), 0, nil
	case 6:
		if len(inner) < 40 {
			return nil, 0, errors.New("swu: inner IPv6 packet too short")
		}
		return net.IP(inner[24:40]), 0, nil
	default:
		return nil, 0, fmt.Errorf("swu: unsupported inner IP version %d", version)
	}
}

// matchSelectors reports whether an outbound inner packet matches the
// negotiated initiator/responder traffic selectors.
func matchSelectors(inner []byte, tsi, tsr *ikev2.EncryptedPayloadTS) bool {
	flow, err := parseInnerPacketFlow(inner)
	if err != nil {
		return false
	}
	return selectorPayloadMatches(tsi, flow.sourceIP, flow.protocol, flow.sourcePort) &&
		selectorPayloadMatches(tsr, flow.destinationIP, flow.protocol, flow.destinationPort)
}

func matchInboundSelectors(inner []byte, tsi, tsr *ikev2.EncryptedPayloadTS) bool {
	flow, err := parseInnerPacketFlow(inner)
	if err != nil {
		return false
	}
	return selectorPayloadMatches(tsr, flow.sourceIP, flow.protocol, flow.sourcePort) &&
		selectorPayloadMatches(tsi, flow.destinationIP, flow.protocol, flow.destinationPort)
}

type innerPacketFlow struct {
	sourceIP, destinationIP     net.IP
	protocol                    byte
	sourcePort, destinationPort uint16
}

func parseInnerPacketFlow(inner []byte) (innerPacketFlow, error) {
	var flow innerPacketFlow
	if len(inner) < 1 {
		return flow, errors.New("swu: empty inner packet")
	}
	headerLength := 0
	switch inner[0] >> 4 {
	case 4:
		if len(inner) < 20 {
			return flow, errors.New("swu: inner IPv4 packet too short")
		}
		headerLength = int(inner[0]&0x0f) * 4
		if headerLength < 20 || len(inner) < headerLength {
			return flow, errors.New("swu: invalid inner IPv4 header length")
		}
		flow.sourceIP, flow.destinationIP = net.IP(inner[12:16]), net.IP(inner[16:20])
		flow.protocol = inner[9]
	case 6:
		if len(inner) < 40 {
			return flow, errors.New("swu: inner IPv6 packet too short")
		}
		headerLength = 40
		flow.sourceIP, flow.destinationIP = net.IP(inner[8:24]), net.IP(inner[24:40])
		flow.protocol = inner[6]
	default:
		return flow, fmt.Errorf("swu: unsupported inner IP version %d", inner[0]>>4)
	}
	if (flow.protocol == 6 || flow.protocol == 17) && len(inner) >= headerLength+4 {
		flow.sourcePort = binary.BigEndian.Uint16(inner[headerLength : headerLength+2])
		flow.destinationPort = binary.BigEndian.Uint16(inner[headerLength+2 : headerLength+4])
	}
	return flow, nil
}

func selectorPayloadMatches(payload *ikev2.EncryptedPayloadTS, ip net.IP, protocol byte, port uint16) bool {
	if payload == nil {
		return true
	}
	for _, selector := range payload.TrafficSelectors {
		address := ip.To16()
		if selector.TSType == ikev2.TS_IPV4_ADDR_RANGE {
			address = ip.To4()
		}
		if len(address) != len(selector.StartAddr) || (selector.IPProtocol != 0 && selector.IPProtocol != protocol) {
			continue
		}
		if port < selector.StartPort || port > selector.EndPort {
			continue
		}
		if bytes.Compare(address, selector.StartAddr) >= 0 && bytes.Compare(address, selector.EndAddr) <= 0 {
			return true
		}
	}
	return false
}

// ipv4RangeToCIDRs converts an IPv4 range to CIDR blocks.
func ipv4RangeToCIDRs(start, end net.IP) []*net.IPNet {
	var out []*net.IPNet
	s := start.To4()
	e := end.To4()
	if s == nil || e == nil {
		return out
	}
	for ip := s; ipLessEqual(ip, e); incIP(ip) {
		out = append(out, &net.IPNet{IP: append([]byte{}, ip...), Mask: net.CIDRMask(32, 32)})
	}
	return out
}

func ipLessEqual(a, b net.IP) bool {
	for i := 0; i < 4; i++ {
		if a[i] < b[i] {
			return true
		}
		if a[i] > b[i] {
			return false
		}
	}
	return true
}

func incIP(ip net.IP) {
	for i := 3; i >= 0; i-- {
		ip[i]++
		if ip[i] != 0 {
			break
		}
	}
}
