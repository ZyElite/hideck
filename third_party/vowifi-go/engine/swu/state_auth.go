package swu

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"

	"github.com/iniwex5/vowifi-go/engine/ikev2"
)

// requiredConfiguredIMSI returns the configured IMSI or an error.
func requiredConfiguredIMSI(cfg *Config) (string, error) {
	if cfg == nil || strings.TrimSpace(cfg.IMSI) == "" {
		return "", errors.New("swu: no IMSI configured for EAP-AKA")
	}
	return cfg.IMSI, nil
}

// normalizeAKAChallengeMode maps the AKA challenge mode string to a canonical
// value ("aka" or "aka'").
func normalizeAKAChallengeMode(mode string) string {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "aka'", "akaprime", "aka-prime":
		return "aka'"
	default:
		return "aka"
	}
}

// currentIKEIdentity returns the IKE IDi payload (type + data) used for the
// AUTH computation (RFC 7296 §2.15).
func (s *Session) currentIKEIdentity() (byte, []byte) {
	// The 3GPP NAI is encoded as ID_RFC822_ADDR (RFC 7296 section 3.5).
	return ikev2.IDTypeRFC822Addr, []byte(s.currentEAPIdentity())
}

// currentEAPIdentity returns the EAP identity (NAI) for the session.
func (s *Session) currentEAPIdentity() string {
	if s.fastReauthCtx != nil && s.fastReauthCtx.CanUseReauth() {
		return s.fastReauthCtx.ReauthID
	}
	imsi := ""
	if s.cfg != nil {
		imsi = s.cfg.IMSI
	}
	return buildNAI(imsi, s.cfg)
}

// buildCPRequestPayload builds the Configuration payload requesting an inner
// IPv4 or IPv6 address (RFC 7296 section 3.15).
func (s *Session) buildCPRequestPayload() *ikev2.EncryptedPayloadCP {
	return &ikev2.EncryptedPayloadCP{
		CFGType: ikev2.CFG_REQUEST,
		Attributes: []*ikev2.CPAttribute{
			{Type: ikev2.CPAttrIP4Address},
			{Type: ikev2.CPAttrIP4DNS},
			{Type: ikev2.CPAttrPCSCFIP4},
			{Type: ikev2.CPAttrIP6Address},
			{Type: ikev2.CPAttrIP6DNS},
			{Type: ikev2.CPAttrPCSCFIP6},
		},
	}
}

// buildTrafficSelectorsForIPStack builds the TSi/TSr traffic selectors for the
// inner IP stack (RFC 7296 §3.13).
func buildTrafficSelectorsForIPStack(innerIP net.IP) (*ikev2.EncryptedPayloadTS, *ikev2.EncryptedPayloadTS) {
	if innerIP == nil {
		return trafficSelectorPayloads(
			[]*ikev2.TrafficSelector{anyIPv4Selector(), anyIPv6Selector()},
			[]*ikev2.TrafficSelector{anyIPv4Selector(), anyIPv6Selector()},
		)
	}
	if ipv4 := innerIP.To4(); ipv4 != nil {
		return trafficSelectorPayloads(
			[]*ikev2.TrafficSelector{ikev2.NewTrafficSelectorIPV4(ipv4, 0, 0, 0xffff)},
			[]*ikev2.TrafficSelector{anyIPv4Selector()},
		)
	}
	return trafficSelectorPayloads(
		[]*ikev2.TrafficSelector{ikev2.NewTrafficSelectorIPV6(innerIP, 0, 0, 0xffff)},
		[]*ikev2.TrafficSelector{anyIPv6Selector()},
	)
}

func trafficSelectorPayloads(initiator, responder []*ikev2.TrafficSelector) (*ikev2.EncryptedPayloadTS, *ikev2.EncryptedPayloadTS) {
	return &ikev2.EncryptedPayloadTS{IsInitiator: true, TrafficSelectors: initiator},
		&ikev2.EncryptedPayloadTS{TrafficSelectors: responder}
}

func anyIPv4Selector() *ikev2.TrafficSelector {
	return ikev2.NewTrafficSelectorIPV4Range(net.IPv4zero, net.IPv4bcast, 0, 0, 0xffff)
}

func anyIPv6Selector() *ikev2.TrafficSelector {
	return ikev2.NewTrafficSelectorIPV6Range(net.IPv6unspecified, net.IP{
		0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff,
		0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff,
	}, 0, 0, 0xffff)
}

// spoofAppleIMEI returns an IMEI string that passes the Luhn check, derived
// from the configured IMEI (recovered from the decompiled spoofAppleIMEI).
func spoofAppleIMEI(imei string) string {
	digits := make([]byte, 0, 15)
	for _, c := range imei {
		if c >= '0' && c <= '9' {
			digits = append(digits, byte(c-'0'))
		}
		if len(digits) == 14 {
			break
		}
	}
	if len(digits) < 14 {
		return imei
	}
	// Compute the Luhn check digit over the first 14 digits: double every
	// second digit starting from the rightmost (index 13, 11, ...).
	sum := 0
	for i := 0; i < 14; i++ {
		d := int(digits[i])
		if i%2 == 1 {
			d *= 2
			if d > 9 {
				d -= 9
			}
		}
		sum += d
	}
	check := (10 - sum%10) % 10
	out := make([]byte, 15)
	for i := 0; i < 14; i++ {
		out[i] = digits[i] + '0'
	}
	out[14] = byte(check) + '0'
	return string(out)
}

// runIKEAuthLoop drives the IKE_AUTH exchange (RFC 7296 §2.9) including the
// EAP-AKA sub-exchange (RFC 4187).
func (s *Session) runIKEAuthLoop(ctx context.Context) error {
	s.setState(stateAuthenticating)
	s.stage = stageInit

	for {
		switch s.stage {
		case stageInit:
			// Build and send the initial IKE_AUTH request (IDi, AUTH, SAi2,
			// TSi, TSr, CP, EAP-Identity).
			payloads, err := s.buildIKEAuthInitPayloads()
			if err != nil {
				return err
			}
			if err := s.sendIKEAuthRequest(payloads); err != nil {
				return err
			}
			s.stage = stageEAP
		case stageEAP:
			// Wait for the IKE_AUTH response (EAP request or final).
			resp, err := s.receiveIKE(ctx)
			if err != nil {
				return err
			}
			decision, err := s.executeIKEAuthDecision(resp)
			if err != nil {
				return err
			}
			switch decision {
			case "eap":
				// Continue the EAP exchange.
			case "final":
				s.stage = stageFinal
			case "done":
				return nil
			default:
				return fmt.Errorf("swu: unexpected IKE_AUTH decision %q", decision)
			}
		case stageFinal:
			// Send the final IKE_AUTH request with AUTH after EAP success.
			payloads, err := s.buildIKEAuthFinalPayloads()
			if err != nil {
				return err
			}
			if err := s.sendIKEAuthRequest(payloads); err != nil {
				return err
			}
			s.stage = stageDone
		case stageDone:
			// Wait for the final IKE_AUTH response (AUTH, SA, TS).
			resp, err := s.receiveIKE(ctx)
			if err != nil {
				return err
			}
			return s.handleIKEAuthFinalResp(resp)
		}
	}
}

// advanceIKEAuthStage advances the IKE_AUTH stage based on the current state.
func (s *Session) advanceIKEAuthStage() error {
	switch s.stage {
	case stageInit:
		s.stage = stageEAP
		return nil
	case stageEAP:
		s.stage = stageFinal
		return nil
	case stageFinal:
		s.stage = stageDone
		return nil
	case stageDone:
		return nil
	default:
		return fmt.Errorf("swu: unknown IKE_AUTH stage %d", s.stage)
	}
}

// sendIKEAuthRequest encrypts and sends an IKE_AUTH request.
func (s *Session) sendIKEAuthRequest(payloads []ikev2.Payload) error {
	pkt := &ikev2.IKEPacket{
		Header:   newIKEHeader(s.SPIi, s.SPIr, ikev2.IKE_AUTH, ikev2.FlagInitiator, s.nextMessageID()),
		Payloads: payloads,
	}
	raw, err := s.encryptAndWrap(pkt)
	if err != nil {
		return err
	}
	return s.sendIKE(raw)
}

// buildIKEAuthInitPayloads builds the initial EAP IKE_AUTH request. RFC 7296
// section 2.16 requires the initiator to omit AUTH until EAP succeeds.
func (s *Session) buildIKEAuthInitPayloads() ([]ikev2.Payload, error) {
	imsi, err := requiredConfiguredIMSI(s.cfg)
	if err != nil {
		return nil, err
	}
	_ = imsi

	// IDi (ID_NAI).
	idType, idData := s.currentIKEIdentity()
	idi := &ikev2.EncryptedPayloadID{IDType: idType, IDData: idData, IsInitiator: true}
	payloads := []ikev2.Payload{idi}
	if apn := strings.TrimSpace(s.cfg.APN); apn != "" {
		idr := &ikev2.EncryptedPayloadID{IDType: ikev2.ID_FQDN, IDData: []byte(apn)}
		payloads = append(payloads, idr)
	}

	if s.espLocalSPI == 0 {
		localSPI, err := randomChildSPI()
		if err != nil {
			return nil, err
		}
		s.espLocalSPI = localSPI
	}

	// SAi2 (ESP proposal) includes the initiator-selected inbound SPI.
	espProposals, err := buildESPProposals(s.cfg, spiBytes(s.espLocalSPI))
	if err != nil {
		return nil, err
	}
	sa2 := &ikev2.EncryptedPayloadSA{Proposals: espProposals}

	// TSi / TSr.
	tsi, tsr := buildTrafficSelectorsForIPStack(nil)

	// CP (request inner address) and RFC 5998 EAP-only authentication. EAP-AKA
	// and EAP-AKA' are mutually authenticating, key-generating methods; the
	// responder's final AUTH is still mandatory and is verified with the MSK.
	cp := s.buildCPRequestPayload()
	eapOnly := &ikev2.EncryptedPayloadNotify{
		NotifyType: ikev2.NotifyTypeEAPOnlyAuthentication,
	}
	s.eapOnlyRequested = true

	return append(payloads, sa2, tsi, tsr, eapOnly, cp), nil
}

// computeInitiatorAuth computes the EAP-only initiator AUTH from the MSK and
// the complete IKE_SA_INIT transcript (RFC 7296 section 2.16).
func (s *Session) computeInitiatorAuth() (*ikev2.EncryptedPayloadAuth, error) {
	return s.computeEAPInitiatorAuth()
}

// executeIKEAuthDecision inspects an IKE_AUTH response and decides the next
// step: "eap" (continue EAP), "final" (EAP success, send final AUTH) or "done"
// (session established without EAP).
func (s *Session) executeIKEAuthDecision(resp *ikev2.IKEPacket) (string, error) {
	payloads, err := s.decryptAndParse(resp)
	if err != nil {
		return "", err
	}
	return s.applyEAPHandlingResult(payloads)
}

// applyEAPHandlingResult processes the decrypted IKE_AUTH response payloads and
// returns the next decision.
func (s *Session) applyEAPHandlingResult(payloads []ikev2.Payload) (string, error) {
	if !s.responderAuthenticated {
		deferred, err := s.authenticateInitialResponder(payloads)
		if err != nil {
			return "", err
		}
		if !deferred {
			s.responderAuthenticated = true
		}
	}
	for _, pl := range payloads {
		switch pl.Type() {
		case ikev2.PayloadEAP:
			eapData, ok := ikeEAPData(pl)
			if !ok {
				return "", errors.New("swu: invalid EAP payload")
			}
			if err := s.handleRFCEAP(eapData); err != nil {
				return "", err
			}
			if s.stage == stageFinal {
				return "final", nil
			}
			return "eap", nil
		}
	}
	return "", errors.New("swu: IKE_AUTH response has no EAP payload")
}

func ikeEAPData(payload ikev2.Payload) ([]byte, bool) {
	switch value := payload.(type) {
	case *ikev2.EncryptedPayloadEAP:
		return value.EAPMessage, true
	case *ikev2.RawPayload:
		return value.Data, true
	default:
		return nil, false
	}
}

// buildIKEAuthFinalPayloads builds the final IKE_AUTH request after EAP
// success. IDi and the CHILD_SA proposal were already sent in the first
// request; this exchange carries only the MSK-authenticated AUTH payload.
func (s *Session) buildIKEAuthFinalPayloads() ([]ikev2.Payload, error) {
	auth, err := s.computeInitiatorAuth()
	if err != nil {
		return nil, err
	}
	return []ikev2.Payload{auth}, nil
}

// handleIKEAuthFinalResp processes the final IKE_AUTH response (AUTH, SA, TS,
// CP) and verifies the responder AUTH.
func (s *Session) handleIKEAuthFinalResp(resp *ikev2.IKEPacket) error {
	payloads, err := s.decryptAndParse(resp)
	if err != nil {
		return err
	}
	if !s.responderAuthenticated {
		if !s.eapOnlyAuthentication {
			return errors.New("swu: responder was not authenticated before EAP completion")
		}
	}
	// The encrypted response is integrity-protected by the IKE SA. Surface a
	// responder rejection before requiring AUTH, because error responses omit it.
	if err := ikeAuthenticationError(payloads); err != nil {
		return err
	}
	if err := s.verifyEAPResponderAuth(payloads); err != nil {
		return err
	}
	s.responderAuthenticated = true
	assigned, err := parseAssignedInnerConfig(payloads)
	if err != nil {
		return err
	}
	offerTSi, offerTSr := buildTrafficSelectorsForIPStack(nil)
	selection, err := validateChildSAResponse(payloads, childSAOffer{
		encryption: s.espCipher, encryptionKeyBits: s.espEncKeyBits, integrity: s.espInteg,
		tsi: offerTSi, tsr: offerTSr, localIPs: assigned.ips(),
	})
	if err != nil {
		return err
	}
	s.innerIP, s.innerIPv6 = assigned.ipv4, assigned.ipv6
	s.innerPrefix, s.innerIPv6Prefix = assigned.ipv4Prefix, assigned.ipv6Prefix
	s.dnsServers, s.pcscfServers = assigned.dns, assigned.pcscf
	if selection != nil {
		s.espRemoteSPI = selection.remoteSPI
		s.espCipher, s.espInteg = selection.encryption, selection.integrity
		s.childNi, s.childNr = append([]byte(nil), s.Ni...), s.Nr()
		s.childTSi, s.childTSr = selection.tsi, selection.tsr
	}
	return nil
}

type assignedInnerConfig struct {
	ipv4       net.IP
	ipv6       net.IP
	ipv4Prefix int
	ipv6Prefix int
	dns        []net.IP
	pcscf      []net.IP
}

func (config assignedInnerConfig) ips() []net.IP {
	var ips []net.IP
	if config.ipv4 != nil {
		ips = append(ips, config.ipv4)
	}
	if config.ipv6 != nil {
		ips = append(ips, config.ipv6)
	}
	return ips
}

func parseAssignedInnerConfig(payloads []ikev2.Payload) (assignedInnerConfig, error) {
	var result assignedInnerConfig
	var cp *ikev2.EncryptedPayloadCP
	for _, payload := range payloads {
		if payload.Type() != ikev2.PayloadCP {
			continue
		}
		value, ok := payload.(*ikev2.EncryptedPayloadCP)
		if !ok || cp != nil || value.CFGType != ikev2.CFG_REPLY {
			return result, errors.New("swu: invalid, duplicate, or non-reply CP payload")
		}
		cp = value
	}
	if cp == nil {
		return result, fmt.Errorf("swu: final IKE_AUTH response omitted CFG_REPLY (payloads=%s)", ikePayloadTypes(payloads))
	}
	config := ikev2.ParseCPConfig(cp)
	if raw, ok := cpAttributeValue(cp, ikev2.INTERNAL_IP4_ADDRESS); ok {
		if len(raw) != net.IPv4len {
			return result, fmt.Errorf("swu: invalid assigned IPv4 length %d", len(raw))
		}
		result.ipv4 = append(net.IP(nil), config.IPv4Addresses[0]...)
		result.ipv4Prefix = ipv4PrefixFromCP(cp)
	}
	if raw, ok := cpAttributeValue(cp, ikev2.INTERNAL_IP6_ADDRESS); ok {
		if len(raw) != net.IPv6len+1 {
			return result, fmt.Errorf("swu: invalid assigned IPv6 length %d", len(raw))
		}
		result.ipv6 = append(net.IP(nil), config.IPv6Addresses[0]...)
		result.ipv6Prefix = int(config.IPv6Prefix)
		if result.ipv6Prefix > net.IPv6len*8 {
			return result, fmt.Errorf("swu: invalid assigned IPv6 prefix %d", result.ipv6Prefix)
		}
	}
	var err error
	result.dns, err = cpIPAddresses(cp, ikev2.CPAttrIP4DNS, ikev2.CPAttrIP6DNS)
	if err != nil {
		return result, err
	}
	result.pcscf, err = cpIPAddresses(cp, ikev2.CPAttrPCSCFIP4, ikev2.CPAttrPCSCFIP6)
	if err != nil {
		return result, err
	}
	if result.ipv4 == nil && result.ipv6 == nil {
		return result, fmt.Errorf("swu: CFG_REPLY omitted an assigned address (attributes=%s)", cpAttributeSummary(cp))
	}
	return result, nil
}

func cpAttributeSummary(cp *ikev2.EncryptedPayloadCP) string {
	if cp == nil || len(cp.Attributes) == 0 {
		return "none"
	}
	attributes := make([]string, 0, len(cp.Attributes))
	for _, attribute := range cp.Attributes {
		if attribute == nil {
			attributes = append(attributes, "nil")
			continue
		}
		attributes = append(attributes, fmt.Sprintf("%d:%d", attribute.Type, len(attribute.Value)))
	}
	return strings.Join(attributes, ",")
}

func ipv4PrefixFromCP(cp *ikev2.EncryptedPayloadCP) int {
	const defaultIPv4Prefix = 32
	maskBytes, _ := cpAttributeValue(cp, ikev2.INTERNAL_IP4_NETMASK)
	if len(maskBytes) < net.IPv4len {
		return defaultIPv4Prefix
	}
	ones, bits := net.IPMask(maskBytes[:net.IPv4len]).Size()
	if bits != 32 || ones == 0 {
		return defaultIPv4Prefix
	}
	return ones
}

func cpAttributeValue(cp *ikev2.EncryptedPayloadCP, attributeType uint16) ([]byte, bool) {
	if cp == nil {
		return nil, false
	}
	for _, attribute := range cp.Attributes {
		if attribute != nil && attribute.Type == attributeType {
			return attribute.Value, true
		}
	}
	return nil, false
}

func cpIPAddresses(cp *ikev2.EncryptedPayloadCP, ipv4Type, ipv6Type uint16) ([]net.IP, error) {
	var addresses []net.IP
	for _, attribute := range cp.Attributes {
		if attribute == nil || attribute.Type != ipv4Type && attribute.Type != ipv6Type {
			continue
		}
		expectedLength := net.IPv6len
		if attribute.Type == ipv4Type {
			expectedLength = net.IPv4len
		}
		if len(attribute.Value) != expectedLength {
			return nil, fmt.Errorf("swu: invalid CP address attribute %d length %d", attribute.Type, len(attribute.Value))
		}
		addresses = append(addresses, append(net.IP(nil), attribute.Value...))
	}
	return addresses, nil
}

// verifyResponderAuth verifies the responder certificate and its signature
// over the RFC 7296 SignedOctets.
func (s *Session) verifyResponderAuth(payloads []ikev2.Payload) error {
	if s.ikeKeys == nil || s.prf == nil {
		return errors.New("swu: no IKE SA keys for AUTH verification")
	}
	return s.verifyResponderCertificateAuth(payloads)
}
