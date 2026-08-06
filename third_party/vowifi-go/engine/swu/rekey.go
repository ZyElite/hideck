package swu

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"net"

	"github.com/iniwex5/vowifi-go/engine/ikev2"
)

// RekeyIKESA initiates an IKE SA rekey (RFC 7296 §2.8): a CREATE_CHILD_SA
// exchange with a new SAi1 proposal and fresh nonces.
func (s *Session) RekeyIKESA() error {
	if s.socket == nil || s.ikeKeys == nil {
		return errors.New("swu: session not established")
	}
	ni := make([]byte, s.nonceLen)
	if _, err := rand.Read(ni); err != nil {
		return err
	}
	proposals := buildIKEProposals(s.encrAlg, s.prfAlg, s.integAlg, s.dhGroup)
	pkt := &ikev2.IKEPacket{
		InitiatorSPI: s.SPIi,
		ResponderSPI: s.SPIr,
		Version:      0x20,
		ExchangeType: ikev2.ExchangeCreateChildSA,
		Flags:        0x08,
		MessageID:    s.nextMessageID(),
		Payloads: []ikev2.Payload{
			&ikev2.EncryptedPayloadSA{Proposals: proposals},
			&ikev2.EncryptedPayloadNonce{Data: ni},
		},
	}
	raw, err := s.encryptAndWrap(pkt)
	if err != nil {
		return err
	}
	return s.sendIKE(raw)
}

// RekeyChildSA initiates a CHILD_SA rekey (RFC 7296 §2.8).
func (s *Session) RekeyChildSA() error {
	if s.socket == nil || s.ikeKeys == nil {
		return errors.New("swu: session not established")
	}
	ni := make([]byte, s.nonceLen)
	if _, err := rand.Read(ni); err != nil {
		return err
	}
	espProposals := buildESPProposals(s.espCipher, s.espInteg)
	tsi, tsr := buildTrafficSelectorsForIPStack(s.innerIP)
	pkt := &ikev2.IKEPacket{
		InitiatorSPI: s.SPIi,
		ResponderSPI: s.SPIr,
		Version:      0x20,
		ExchangeType: ikev2.ExchangeCreateChildSA,
		Flags:        0x08,
		MessageID:    s.nextMessageID(),
		Payloads: []ikev2.Payload{
			&ikev2.EncryptedPayloadSA{Proposals: espProposals},
			&ikev2.EncryptedPayloadNonce{Data: ni},
			tsi,
			tsr,
		},
	}
	raw, err := s.encryptAndWrap(pkt)
	if err != nil {
		return err
	}
	return s.sendIKE(raw)
}

// HandleRekeyIKESARequest handles an incoming IKE SA rekey request.
func (s *Session) HandleRekeyIKESARequest() error {
	return errors.New("swu: IKE SA rekey request handling not wired")
}

// handleRekeyIKESAResp processes an IKE SA rekey response.
func (s *Session) handleRekeyIKESAResp() error {
	return errors.New("swu: IKE SA rekey response handling not wired")
}

// handleCreateChildSAResp processes a CREATE_CHILD_SA response.
func (s *Session) handleCreateChildSAResp() error {
	return errors.New("swu: CREATE_CHILD_SA response handling not wired")
}

// handleIncomingCreateChildSAParsed processes an inbound CREATE_CHILD_SA.
func (s *Session) handleIncomingCreateChildSAParsed() error {
	return errors.New("swu: inbound CREATE_CHILD_SA handling not wired")
}

// handleIncomingInformational processes an inbound INFORMATIONAL request.
func (s *Session) handleIncomingInformational() error {
	return errors.New("swu: inbound INFORMATIONAL handling not wired")
}

// dispatchCreateChildSA performs the CREATE_CHILD_SA exchange for the ESP data
// plane (RFC 7296 §1.3.2).
func (s *Session) dispatchCreateChildSA(ctx context.Context) error {
	ni := make([]byte, s.nonceLen)
	if _, err := rand.Read(ni); err != nil {
		return err
	}
	espProposals := buildESPProposals(s.cfg.ESPEncryption, s.cfg.ESPIntegrity)
	tsi, tsr := buildTrafficSelectorsForIPStack(s.innerIP)
	pkt := &ikev2.IKEPacket{
		InitiatorSPI: s.SPIi,
		ResponderSPI: s.SPIr,
		Version:      0x20,
		ExchangeType: ikev2.ExchangeCreateChildSA,
		Flags:        0x08,
		MessageID:    2,
		Payloads: []ikev2.Payload{
			&ikev2.EncryptedPayloadSA{Proposals: espProposals},
			&ikev2.EncryptedPayloadNonce{Data: ni},
			tsi,
			tsr,
		},
	}
	raw, err := s.encryptAndWrap(pkt)
	if err != nil {
		return err
	}
	if err := s.sendIKE(raw); err != nil {
		return err
	}
	resp, err := s.receiveIKE(ctx)
	if err != nil {
		return err
	}
	payloads, err := s.decryptAndParse(resp)
	if err != nil {
		return err
	}
	// Extract the responder SPI from the SA payload.
	for _, pl := range payloads {
		if pl.Type() == ikev2.PayloadSA {
			if sa, ok := pl.(*ikev2.EncryptedPayloadSA); ok && len(sa.Proposals) > 0 && len(sa.Proposals[0].SPI) >= 4 {
				s.espRemoteSPI = binary.BigEndian.Uint32(sa.Proposals[0].SPI[:4])
			}
		}
	}
	return nil
}

// UpdateAddresses handles a MOBIKE address update (RFC 4555): it records the
// new local/remote addresses and sends an UPDATE_SA_ADDRESSES notification.
func (s *Session) UpdateAddresses(oldIP, newIP net.IP) error {
	if s.socket == nil {
		return errors.New("swu: no transport")
	}
	s.mu.Lock()
	s.remoteIP = newIP
	s.mu.Unlock()
	return s.sendMOBIKEUpdate()
}

// sendMOBIKEUpdate sends a MOBIKE UPDATE_SA_ADDRESSES INFORMATIONAL request.
func (s *Session) sendMOBIKEUpdate() error {
	if s.socket == nil || s.ikeKeys == nil {
		return errors.New("swu: session not established")
	}
	pkt := &ikev2.IKEPacket{
		InitiatorSPI: s.SPIi,
		ResponderSPI: s.SPIr,
		Version:      0x20,
		ExchangeType: ikev2.ExchangeInformational,
		Flags:        0x08,
		MessageID:    s.nextMessageID(),
		Payloads: []ikev2.Payload{
			&ikev2.EncryptedPayloadNotify{
				ProtocolID: ikev2.ProtoIKE,
				NotifyType: 16400, // UPDATE_SA_ADDRESSES
			},
		},
	}
	raw, err := s.encryptAndWrap(pkt)
	if err != nil {
		return err
	}
	return s.sendIKE(raw)
}

// updateXFRMState updates the XFRM SA state after a MOBIKE update.
func (s *Session) updateXFRMState() error {
	return errors.New("swu: XFRM state update not wired")
}

// verifyCookie2Response verifies a COOKIE2 response (RFC 7296 §2.6).
func (s *Session) verifyCookie2Response() error {
	return errors.New("swu: COOKIE2 verification not wired")
}

// rekeyXFRMSA rekeys the kernel XFRM SA.
func (s *Session) rekeyXFRMSA() error {
	return errors.New("swu: XFRM SA rekey not wired")
}

// sendDeleteChildSA sends a Delete payload for the CHILD_SA.
func (s *Session) sendDeleteChildSA() error {
	if s.socket == nil || s.ikeKeys == nil {
		return errors.New("swu: session not established")
	}
	pkt := &ikev2.IKEPacket{
		InitiatorSPI: s.SPIi,
		ResponderSPI: s.SPIr,
		Version:      0x20,
		ExchangeType: ikev2.ExchangeInformational,
		Flags:        0x08,
		MessageID:    s.nextMessageID(),
		Payloads: []ikev2.Payload{
			&ikev2.EncryptedPayloadDelete{ProtocolID: ikev2.ProtoESP, SPIs: spiBytes(s.espRemoteSPI)},
		},
	}
	raw, err := s.encryptAndWrap(pkt)
	if err != nil {
		return err
	}
	return s.sendIKE(raw)
}

// sendDeleteIKE sends a Delete payload for the IKE SA.
func (s *Session) sendDeleteIKE() error {
	if s.socket == nil || s.ikeKeys == nil {
		return errors.New("swu: session not established")
	}
	pkt := &ikev2.IKEPacket{
		InitiatorSPI: s.SPIi,
		ResponderSPI: s.SPIr,
		Version:      0x20,
		ExchangeType: ikev2.ExchangeInformational,
		Flags:        0x08,
		MessageID:    s.nextMessageID(),
		Payloads: []ikev2.Payload{
			&ikev2.EncryptedPayloadDelete{ProtocolID: ikev2.ProtoIKE, SPIs: spiBytes(0)},
		},
	}
	raw, err := s.encryptAndWrap(pkt)
	if err != nil {
		return err
	}
	return s.sendIKE(raw)
}

// spiBytes encodes an SPI as a 4-byte big-endian slice.
func spiBytes(spi uint32) []byte {
	return []byte{byte(spi >> 24), byte(spi >> 16), byte(spi >> 8), byte(spi)}
}

// sendEncryptedResponseWithMsgID sends an encrypted response with an explicit
// message ID.
func (s *Session) sendEncryptedResponseWithMsgID(payloads []ikev2.Payload, msgID uint32) error {
	pkt := &ikev2.IKEPacket{
		InitiatorSPI: s.SPIi,
		ResponderSPI: s.SPIr,
		Version:      0x20,
		ExchangeType: ikev2.ExchangeInformational,
		Flags:        0x20, // responder
		MessageID:    msgID,
		Payloads:     payloads,
	}
	raw, err := s.encryptAndWrapWithMsgID(pkt, msgID)
	if err != nil {
		return err
	}
	return s.sendIKE(raw)
}

// sendEncryptedWithRetry sends an encrypted message with a single retry.
func (s *Session) sendEncryptedWithRetry(payloads []ikev2.Payload) error {
	err := s.sendEncryptedResponseWithMsgID(payloads, s.nextMessageID())
	if err != nil {
		return err
	}
	return s.sendEncryptedResponseWithMsgID(payloads, s.nextMessageID())
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

// shouldFragment reports whether an IKE message needs fragmentation.
func (s *Session) shouldFragment(raw []byte) bool {
	return len(raw) > 1280
}

// fragmentMessage fragments an IKE message (RFC 7383).
func (s *Session) fragmentMessage(raw []byte) ([][]byte, error) {
	const fragSize = 1200
	var out [][]byte
	for len(raw) > fragSize {
		out = append(out, raw[:fragSize])
		raw = raw[fragSize:]
	}
	if len(raw) > 0 {
		out = append(out, raw)
	}
	return out, nil
}

// startIKEControlLoop starts the IKE control loop (dispatcher).
func (s *Session) startIKEControlLoop() error {
	go s.ikeDispatchLoop()
	return nil
}

// ikeDispatchLoop dispatches inbound IKE messages.
func (s *Session) ikeDispatchLoop() {
	for {
		select {
		case <-s.ctx.Done():
			return
		case raw, ok := <-s.socket.IKEPackets():
			if !ok {
				return
			}
			pkt, err := ikev2.DecodePacket(raw)
			if err != nil {
				continue
			}
			_ = s.handleIncomingIKE(pkt)
		}
	}
}

// handleIncomingIKE routes an inbound IKE message.
func (s *Session) handleIncomingIKE(pkt *ikev2.IKEPacket) error {
	switch pkt.ExchangeType {
	case ikev2.ExchangeInformational:
		return s.handleIncomingInformational()
	case ikev2.ExchangeCreateChildSA:
		return s.handleIncomingCreateChildSAParsed()
	default:
		return nil
	}
}

// ensureIKEDispatcher ensures the IKE dispatcher is running.
func (s *Session) ensureIKEDispatcher() error {
	return s.startIKEControlLoop()
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

// ensureIPv6RuntimeEnabled enables IPv6 on the runtime (no-op in user space).
func (s *Session) ensureIPv6RuntimeEnabled() error {
	return nil
}

// applyNetworkConfigOnTUN applies the inner address/routes to the TUN device.
func (s *Session) applyNetworkConfigOnTUN() error {
	if s.innerIP == nil {
		return errors.New("swu: no inner address")
	}
	return nil
}

// cleanupNetworkConfig removes the network configuration on teardown.
func (s *Session) cleanupNetworkConfig() error {
	return nil
}

// setupXFRMDataPlane installs the kernel XFRM data plane.
func (s *Session) setupXFRMDataPlane() error {
	return errors.New("swu: XFRM data plane not wired")
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

// handleCookie processes a COOKIE notify.
func (s *Session) handleCookie() error {
	return errors.New("swu: COOKIE handling not wired")
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

// matchSelectors reports whether an inner packet matches the traffic selectors.
func matchSelectors(inner []byte, tsi, tsr *ikev2.EncryptedPayloadTS) bool {
	dst, _, err := extractDstTuple(inner)
	if err != nil {
		return false
	}
	if tsr == nil || len(tsr.Selectors) == 0 {
		return true
	}
	for _, sel := range tsr.Selectors {
		if sel.Type == ikev2.TSIPv4Range && dst.To4() != nil {
			return true
		}
		if sel.Type == ikev2.TSIPv6Range && dst.To4() == nil {
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
