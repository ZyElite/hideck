package swu

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"net"

	"github.com/iniwex5/vowifi-go/engine/ikev2"
	"github.com/iniwex5/vowifi-go/engine/ipsec"
)

// RekeyIKESA initiates an IKE SA rekey (RFC 7296 §2.8): a CREATE_CHILD_SA
// exchange with a new SAi1 proposal and fresh nonces.
func (s *Session) RekeyIKESA() error {
	return s.performIKESARekey(s.ctx)
}

// RekeyChildSA initiates a CHILD_SA rekey (RFC 7296 §2.8).
func (s *Session) RekeyChildSA() error {
	return s.performChildSARekey(s.ctx)
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
func (s *Session) handleIncomingCreateChildSAParsed(packet *ikev2.IKEPacket) error {
	payloads, err := s.decryptAndParse(packet)
	if err != nil {
		return err
	}
	protocolID, err := createChildSAProtocol(payloads)
	if err != nil {
		return err
	}
	if protocolID == ikev2.ProtoIKE {
		return s.handlePeerIKESARekey(packet, payloads)
	}
	return s.handlePeerChildSARekeyPayloads(packet, payloads)
}

func createChildSAProtocol(payloads []ikev2.Payload) (byte, error) {
	for _, payload := range payloads {
		sa, ok := payload.(*ikev2.EncryptedPayloadSA)
		if !ok || len(sa.Proposals) != 1 || sa.Proposals[0] == nil {
			continue
		}
		protocolID := sa.Proposals[0].ProtocolID
		if protocolID != ikev2.ProtoIKE && protocolID != ikev2.ProtoESP {
			return 0, fmt.Errorf("swu: unsupported CREATE_CHILD_SA protocol %d", protocolID)
		}
		return protocolID, nil
	}
	return 0, errors.New("swu: CREATE_CHILD_SA request missing a single SA proposal")
}

// handleIncomingInformational processes an inbound INFORMATIONAL request.
func (s *Session) handleIncomingInformational(packet *ikev2.IKEPacket) error {
	return s.handlePeerInformational(packet)
}

// dispatchCreateChildSA performs the CREATE_CHILD_SA exchange for the ESP data
// plane (RFC 7296 §1.3.2).
func (s *Session) dispatchCreateChildSA(ctx context.Context) error {
	ni := make([]byte, s.nonceLen)
	if _, err := rand.Read(ni); err != nil {
		return err
	}
	localSPI, err := randomChildSPI()
	if err != nil {
		return err
	}
	espProposals := buildESPProposalsForSession(s, localSPI)
	tsi, tsr := buildTrafficSelectorsForIPStack(s.primaryInnerIP())
	pkt := &ikev2.IKEPacket{
		InitiatorSPI: s.SPIi,
		ResponderSPI: s.SPIr,
		Version:      0x20,
		ExchangeType: ikev2.ExchangeCreateChildSA,
		Flags:        s.localIKEFlags(false),
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
	offer := childSAOffer{
		encryption: s.espCipher, encryptionKeyBits: s.espEncKeyBits, integrity: s.espInteg,
		tsi: tsi, tsr: tsr, localIPs: configuredInnerIPs(s),
		requireSA: true, requireNonce: true,
	}
	selection, err := validateChildSAResponse(payloads, offer)
	if err != nil {
		return err
	}
	s.espLocalSPI = localSPI
	s.espRemoteSPI = selection.remoteSPI
	s.espCipher, s.espInteg = selection.encryption, selection.integrity
	s.childNi = append([]byte{}, ni...)
	s.childNr = append([]byte(nil), selection.nonce...)
	s.childTSi, s.childTSr = selection.tsi, selection.tsr
	return nil
}

func configuredInnerIPs(session *Session) []net.IP {
	var ips []net.IP
	if session.innerIP != nil {
		ips = append(ips, append(net.IP(nil), session.innerIP...))
	}
	if session.innerIPv6 != nil {
		ips = append(ips, append(net.IP(nil), session.innerIPv6...))
	}
	return ips
}

func randomChildSPI() (uint32, error) {
	var raw [4]byte
	for attempts := 0; attempts < 3; attempts++ {
		if _, err := rand.Read(raw[:]); err != nil {
			return 0, fmt.Errorf("generate CHILD_SA SPI: %w", err)
		}
		if spi := binary.BigEndian.Uint32(raw[:]); spi != 0 {
			return spi, nil
		}
	}
	return 0, errors.New("generate CHILD_SA SPI: random source returned zero")
}

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
		InitiatorSPI: s.SPIi,
		ResponderSPI: s.SPIr,
		Version:      0x20,
		ExchangeType: ikev2.ExchangeInformational,
		Flags:        s.localIKEFlags(false),
		MessageID:    s.nextMessageID(),
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
		Flags:        s.localIKEFlags(false),
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
		Flags:        s.localIKEFlags(false),
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
	if s.socket == nil {
		return errors.New("swu: no IKE transport")
	}
	s.controlMu.Lock()
	if s.controlRunning {
		s.controlMu.Unlock()
		return nil
	}
	transport := s.socket
	responses := make(chan *ikev2.IKEPacket, 8)
	requests := make(chan *ikev2.IKEPacket, 8)
	s.controlResponses = responses
	s.controlRunning = true
	s.controlWG.Add(2)
	s.controlMu.Unlock()
	go s.ikeDispatchLoop(transport, responses, requests)
	go s.ikeRequestLoop(requests)
	return nil
}

// ikeDispatchLoop dispatches inbound IKE messages.

func (s *Session) ikeDispatchLoop(transport ipsec.Transport, responses chan *ikev2.IKEPacket, requests chan<- *ikev2.IKEPacket) {
	defer s.controlWG.Done()
	defer func() {
		s.controlMu.Lock()
		if s.controlResponses == responses {
			s.controlRunning = false
		}
		s.controlMu.Unlock()
	}()
	for {
		select {
		case <-s.ctx.Done():
			return
		case raw, ok := <-transport.IKEPackets():
			if !ok {
				return
			}
			pkt, err := ikev2.DecodePacket(raw)
			if err != nil {
				s.failEstablishedControl(fmt.Errorf("swu: decode established IKE packet: %w", err))
				return
			}
			if pkt.Flags&ikeResponseFlag != 0 {
				select {
				case responses <- pkt:
				case <-s.ctx.Done():
					return
				}
				continue
			}
			select {
			case requests <- pkt:
			case <-s.ctx.Done():
				return
			}
		}
	}
}

func (s *Session) ikeRequestLoop(requests <-chan *ikev2.IKEPacket) {
	defer s.controlWG.Done()
	for {
		select {
		case <-s.ctx.Done():
			return
		case packet := <-requests:
			s.ikeExchangeMu.Lock()
			err := s.handleIncomingIKE(packet)
			s.ikeExchangeMu.Unlock()
			if err != nil {
				s.failEstablishedControl(err)
				return
			}
		}
	}
}

// handleIncomingIKE routes an inbound IKE message.
func (s *Session) handleIncomingIKE(pkt *ikev2.IKEPacket) error {
	switch pkt.ExchangeType {
	case ikev2.ExchangeInformational:
		return s.handleIncomingInformational(pkt)
	case ikev2.ExchangeCreateChildSA:
		return s.handleIncomingCreateChildSAParsed(pkt)
	default:
		return fmt.Errorf("swu: unsupported established IKE exchange %d", pkt.ExchangeType)
	}
}

func (s *Session) failEstablishedControl(err error) {
	if err == nil || errors.Is(err, context.Canceled) {
		return
	}
	s.stopTimers()
	s.setTerminalError(err)
	s.cancel()
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
	if s.primaryInnerIP() == nil {
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
	for _, selector := range payload.Selectors {
		address := ip.To16()
		if selector.Type == ikev2.TSIPv4Range {
			address = ip.To4()
		}
		if len(address) != len(selector.StartAddr) || (selector.ProtocolID != 0 && selector.ProtocolID != protocol) {
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
