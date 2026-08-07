package swu

import (
	"context"
	"crypto/hmac"
	"crypto/md5"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"strings"

	"github.com/iniwex5/vowifi-go/engine/eap"
	"github.com/iniwex5/vowifi-go/engine/ikev2"
)

// EAP-AKA identity prefixes (RFC 4187 §4.1).
const (
	eapAKAPrefix      = "0" // EAP-AKA
	eapAKAPrimePrefix = "1" // EAP-AKA'
)

// requiredConfiguredIMSI returns the configured IMSI or an error.
func requiredConfiguredIMSI(cfg *Config) (string, error) {
	if cfg == nil || strings.TrimSpace(cfg.IMSI) == "" {
		return "", errors.New("swu: no IMSI configured for EAP-AKA")
	}
	return cfg.IMSI, nil
}

// unexpectedEAPMethodError formats an error for an unexpected EAP method.
func unexpectedEAPMethodError(got byte) error {
	return fmt.Errorf("swu: unexpected EAP method %d", got)
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
	// IDi = ID_NAI (type 9) carrying the EAP identity NAI.
	return 9, []byte(s.currentEAPIdentity())
}

// currentEAPIdentity returns the EAP identity (NAI) for the session.
func (s *Session) currentEAPIdentity() string {
	imsi := ""
	if s.cfg != nil {
		imsi = s.cfg.IMSI
	}
	return buildNAI(imsi, s.cfg.MCC, s.cfg.MNC)
}

// currentEAPIdentityForKeyDerivation returns the identity used for EAP-AKA key
// derivation (the NAI without the leading EAP-AKA prefix).
func (s *Session) currentEAPIdentityForKeyDerivation() string {
	id := s.currentEAPIdentity()
	if len(id) > 1 && (id[0] == '0' || id[0] == '1') {
		return id[1:]
	}
	return id
}

// buildCPRequestPayload builds the Configuration payload requesting an inner
// IPv4/IPv6 address (RFC 7296 §3.15).
func (s *Session) buildCPRequestPayload() *ikev2.EncryptedPayloadCP {
	return &ikev2.EncryptedPayloadCP{
		ConfigType: ikev2.CPTypeRequest,
		Attrs: []*ikev2.CPAttribute{
			{Type: ikev2.CPAttrIP4Address},
			{Type: ikev2.CPAttrIP6Address},
		},
	}
}

// buildTrafficSelectorsForIPStack builds the TSi/TSr traffic selectors for the
// inner IP stack (RFC 7296 §3.13).
func buildTrafficSelectorsForIPStack(innerIP net.IP) (*ikev2.EncryptedPayloadTS, *ikev2.EncryptedPayloadTS) {
	tsi := &ikev2.EncryptedPayloadTS{Selectors: []*ikev2.TrafficSelector{
		ikev2.NewTrafficSelectorIPV4(net.IPv4zero, 0, 0, 0xffff),
	}}
	tsr := &ikev2.EncryptedPayloadTS{Selectors: []*ikev2.TrafficSelector{
		ikev2.NewTrafficSelectorIPV4(net.IPv4zero, 0, 0, 0xffff),
	}}
	if innerIP != nil && innerIP.To4() == nil {
		tsi.Selectors[0] = ikev2.NewTrafficSelectorIPV6(net.IPv6unspecified, 0, 0, 0xffff)
		tsr.Selectors[0] = ikev2.NewTrafficSelectorIPV6(net.IPv6unspecified, 0, 0, 0xffff)
	}
	return tsi, tsr
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
		InitiatorSPI: s.SPIi,
		ResponderSPI: s.SPIr,
		Version:      0x20,
		ExchangeType: ikev2.ExchangeIKEAuth,
		Flags:        0x08,
		MessageID:    s.nextMessageID(),
		Payloads:     payloads,
	}
	raw, err := s.encryptAndWrap(pkt)
	if err != nil {
		return err
	}
	return s.sendIKE(raw)
}

// buildIKEAuthInitPayloads builds the initial IKE_AUTH request payload chain:
// IDi, AUTH, SAi2, TSi, TSr, CP, EAP-Identity.
func (s *Session) buildIKEAuthInitPayloads() ([]ikev2.Payload, error) {
	imsi, err := requiredConfiguredIMSI(s.cfg)
	if err != nil {
		return nil, err
	}
	_ = imsi

	// IDi (ID_NAI).
	idType, idData := s.currentIKEIdentity()
	idi := &ikev2.EncryptedPayloadID{IDType: idType, Data: idData}

	// AUTH = prf(SK_pi, IDi').
	auth, err := s.computeInitiatorAuth()
	if err != nil {
		return nil, err
	}

	if s.espLocalSPI == 0 {
		localSPI, err := randomChildSPI()
		if err != nil {
			return nil, err
		}
		s.espLocalSPI = localSPI
	}

	// SAi2 (ESP proposal) includes the initiator-selected inbound SPI.
	espProposals := buildESPProposals(s.espCipher, s.espInteg, s.espLocalSPI)
	sa2 := &ikev2.EncryptedPayloadSA{Proposals: espProposals}

	// TSi / TSr.
	tsi, tsr := buildTrafficSelectorsForIPStack(s.innerIP)

	// CP (request inner address).
	cp := s.buildCPRequestPayload()

	// EAP-Identity request.
	eapID := byte(1)
	s.eapID = eapID
	eapIdentity := &eap.EAPPacket{
		Code:       eap.CodeResponse,
		Identifier: eapID,
		Type:       eap.TypeIdentity,
		Data:       []byte(s.currentEAPIdentity()),
	}
	eapPayload := &ikev2.EncryptedPayloadEAP{Data: eapIdentity.Encode()}

	return []ikev2.Payload{idi, auth, sa2, tsi, tsr, cp, eapPayload}, nil
}

// computeInitiatorAuth computes the initiator AUTH payload (RFC 7296 §2.15):
//
//	AUTH = prf(SK_pi, IDi')
func (s *Session) computeInitiatorAuth() (*ikev2.EncryptedPayloadAuth, error) {
	if s.ikeKeys == nil || s.prf == nil {
		return nil, errors.New("swu: no IKE SA keys for AUTH")
	}
	idType, idData := s.currentIKEIdentity()
	idi := append([]byte{idType}, idData...)
	auth := s.prf.Compute(s.ikeKeys.SK_pi, idi)
	return &ikev2.EncryptedPayloadAuth{AuthMethod: ikev2.AuthMethodRSA, Data: auth}, nil
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
	for _, pl := range payloads {
		switch pl.Type() {
		case ikev2.PayloadEAP:
			eapData, ok := ikeEAPData(pl)
			if !ok {
				return "", errors.New("swu: invalid EAP payload")
			}
			if err := s.handleEAP(eapData); err != nil {
				return "", err
			}
			if s.stage == stageFinal {
				return "final", nil
			}
			return "eap", nil
		case ikev2.PayloadAuth:
			return "done", nil
		}
	}
	return "", errors.New("swu: IKE_AUTH response has no EAP or AUTH payload")
}

func ikeEAPData(payload ikev2.Payload) ([]byte, bool) {
	switch value := payload.(type) {
	case *ikev2.EncryptedPayloadEAP:
		return value.Data, true
	case *ikev2.RawPayload:
		return value.Data, true
	default:
		return nil, false
	}
}

// handleEAP processes an EAP packet from the IKE_AUTH response and sends the
// appropriate EAP response.
func (s *Session) handleEAP(data []byte) error {
	pkt, err := eap.Parse(data, 0)
	if err != nil {
		return fmt.Errorf("parse EAP: %w", err)
	}
	switch pkt.Code {
	case eap.CodeRequest:
		return s.handleEAPRequest(pkt)
	case eap.CodeSuccess:
		// EAP success: the next IKE_AUTH request carries AUTH.
		s.stage = stageFinal
		return nil
	case eap.CodeFailure:
		return errors.New("swu: EAP authentication failed")
	default:
		return fmt.Errorf("swu: unexpected EAP code %d", pkt.Code)
	}
}

// handleEAPRequest processes an EAP-Request.
func (s *Session) handleEAPRequest(pkt *eap.EAPPacket) error {
	s.eapID = pkt.Identifier
	switch pkt.Type {
	case eap.TypeIdentity:
		// Respond with the EAP identity (NAI).
		resp := &eap.EAPPacket{
			Code:       eap.CodeResponse,
			Identifier: pkt.Identifier,
			Type:       eap.TypeIdentity,
			Data:       []byte(s.currentEAPIdentity()),
		}
		return s.sendEAPResponse(resp)
	case eap.TypeAKA, eap.TypeAKAPrime:
		s.eapType = pkt.Type
		return s.handleAKAChallenge(pkt)
	default:
		return unexpectedEAPMethodError(pkt.Type)
	}
}

// handleAKAChallenge processes an EAP-AKA/AKA' challenge (RFC 4187 §5.1).
func (s *Session) handleAKAChallenge(pkt *eap.EAPPacket) error {
	attrs, err := eap.ParseAttributes(pkt.Data, 0)
	if err != nil {
		return fmt.Errorf("parse AKA attributes: %w", err)
	}
	randAttr := attrs[eap.AttrATRAND]
	autnAttr := attrs[eap.AttrATAUTN]
	if randAttr == nil || autnAttr == nil {
		return errors.New("swu: AKA challenge missing RAND or AUTN")
	}

	// Compute AKA via the provider.
	if s.cfg == nil || s.cfg.AKAProvider == nil {
		return errors.New("swu: no AKA provider configured")
	}
	aka, err := s.cfg.AKAProvider.CalculateAKA(randAttr.Value, autnAttr.Value)
	if err != nil {
		// Sync failure: respond with AUTS.
		if len(aka.AUTS) > 0 {
			return s.buildEAPSyncFailure(pkt, aka.AUTS)
		}
		return fmt.Errorf("swu: AKA computation failed: %w", err)
	}

	// Verify the server MAC (AT_MAC) over the challenge.
	if err := verifyEAPAKAMAC(pkt, attrs, aka.CK, aka.IK, s.eapType); err != nil {
		return fmt.Errorf("swu: EAP-AKA MAC verification failed: %w", err)
	}

	// Build the EAP-AKA response: AT_RES, AT_MAC.
	resp, err := buildSignedEAPResponse(pkt, attrs, aka, s.eapType)
	if err != nil {
		return err
	}
	return s.sendEAPResponse(resp)
}

// buildEAPSyncFailure builds an EAP-AKA sync-failure response carrying AUTS.
func (s *Session) buildEAPSyncFailure(pkt *eap.EAPPacket, auts []byte) error {
	// AT_AUTS attribute (type 0x04 in EAP-AKA, RFC 4187 §9.4).
	body := make([]byte, 0, 2+len(auts))
	body = append(body, 0x04, byte((2+len(auts)+3)/4))
	body = append(body, auts...)
	for len(body)%4 != 0 {
		body = append(body, 0)
	}
	resp := &eap.EAPPacket{
		Code:       eap.CodeResponse,
		Identifier: pkt.Identifier,
		Type:       pkt.Type,
		SubType:    eap.SubtypeSyncFailure,
		Data:       body,
	}
	return s.sendEAPResponse(resp)
}

// sendEAPResponse sends an EAP response inside an IKE_AUTH request.
func (s *Session) sendEAPResponse(resp *eap.EAPPacket) error {
	payloads := []ikev2.Payload{
		&ikev2.EncryptedPayloadEAP{Data: resp.Encode()},
	}
	return s.sendIKEAuthRequest(payloads)
}

// buildIKEAuthFinalPayloads builds the final IKE_AUTH request after EAP
// success: IDi, AUTH, [CP].
func (s *Session) buildIKEAuthFinalPayloads() ([]ikev2.Payload, error) {
	auth, err := s.computeInitiatorAuth()
	if err != nil {
		return nil, err
	}
	idType, idData := s.currentIKEIdentity()
	idi := &ikev2.EncryptedPayloadID{IDType: idType, Data: idData}
	cp := s.buildCPRequestPayload()
	return []ikev2.Payload{idi, auth, cp}, nil
}

// handleIKEAuthFinalResp processes the final IKE_AUTH response (AUTH, SA, TS,
// CP) and verifies the responder AUTH.
func (s *Session) handleIKEAuthFinalResp(resp *ikev2.IKEPacket) error {
	payloads, err := s.decryptAndParse(resp)
	if err != nil {
		return err
	}
	if err := s.verifyResponderAuth(payloads); err != nil {
		return err
	}
	// Extract the inner address from the CP payload.
	for _, pl := range payloads {
		switch pl.Type() {
		case ikev2.PayloadSA:
			sa, ok := pl.(*ikev2.EncryptedPayloadSA)
			if ok && len(sa.Proposals) > 0 && len(sa.Proposals[0].SPI) == 4 {
				s.espRemoteSPI = binary.BigEndian.Uint32(sa.Proposals[0].SPI)
				s.childNi = append([]byte{}, s.Ni...)
				s.childNr = s.Nr()
			}
		case ikev2.PayloadCP:
			if cp, ok := pl.(*ikev2.EncryptedPayloadCP); ok {
				cfg := ikev2.ParseCPConfig(cp)
				if cfg != nil {
					if cfg.HasIPv4() {
						s.innerIP = cfg.IPv4
						s.innerPrefix = ipv4PrefixFromCP(cfg)
					}
					if cfg.HasIPv6() {
						s.innerIPv6 = cfg.IPv6
					}
					s.dnsServers = dnsServersFromCP(cfg)
				}
			}
		}
	}
	return nil
}

func ipv4PrefixFromCP(cfg *ikev2.CPConfig) int {
	const defaultIPv4Prefix = 32
	maskBytes := cfg.Attrs[ikev2.CPAttrIP4Netmask]
	if len(maskBytes) < net.IPv4len {
		return defaultIPv4Prefix
	}
	ones, bits := net.IPMask(maskBytes[:net.IPv4len]).Size()
	if bits != 32 || ones == 0 {
		return defaultIPv4Prefix
	}
	return ones
}

func dnsServersFromCP(cfg *ikev2.CPConfig) []net.IP {
	var servers []net.IP
	if raw := cfg.Attrs[ikev2.CPAttrIP4DNS]; len(raw) >= net.IPv4len {
		servers = append(servers, append(net.IP(nil), raw[:net.IPv4len]...))
	}
	if raw := cfg.Attrs[ikev2.CPAttrIP6DNS]; len(raw) >= net.IPv6len {
		servers = append(servers, append(net.IP(nil), raw[:net.IPv6len]...))
	}
	return servers
}

// verifyResponderAuth verifies the responder AUTH payload (RFC 7296 §2.15):
//
//	AUTH = prf(SK_pr, IDr')
func (s *Session) verifyResponderAuth(payloads []ikev2.Payload) error {
	if s.ikeKeys == nil || s.prf == nil {
		return errors.New("swu: no IKE SA keys for AUTH verification")
	}
	var idrType byte
	var idrData []byte
	var authData []byte
	for _, pl := range payloads {
		switch pl.Type() {
		case ikev2.PayloadIDr:
			if id, ok := pl.(*ikev2.EncryptedPayloadID); ok {
				idrType = id.IDType
				idrData = id.Data
			}
		case ikev2.PayloadAuth:
			if a, ok := pl.(*ikev2.EncryptedPayloadAuth); ok {
				authData = a.Data
			}
		}
	}
	if len(authData) == 0 {
		if s.canAcceptMissingResponderAuth() {
			return nil
		}
		return errors.New("swu: IKE_AUTH response missing AUTH payload")
	}
	if len(idrData) == 0 {
		return errors.New("swu: IKE_AUTH response missing IDr payload")
	}
	idr := append([]byte{idrType}, idrData...)
	expected := s.prf.Compute(s.ikeKeys.SK_pr, idr)
	if !hmac.Equal(expected, authData) {
		s.logResponderAuthMismatch()
		return errors.New("swu: responder AUTH verification failed")
	}
	return nil
}

// logResponderAuthMismatch logs the AUTH mismatch (no-op without a debugger).
func (s *Session) logResponderAuthMismatch() {
	if s.debug != nil {
		s.debug.LogRaw("responder AUTH mismatch")
	}
}

// buildSignedEAPResponse builds the EAP-AKA response to a challenge: AT_RES,
// AT_MAC (RFC 4187 §5.1).
func buildSignedEAPResponse(req *eap.EAPPacket, attrs map[byte]*eap.EAPAttribute, aka AKAResult, eapType byte) (*eap.EAPPacket, error) {
	// AT_RES attribute (type 0x03).
	resAttr := make([]byte, 0, 2+len(aka.RES))
	resAttr = append(resAttr, 0x03, byte((2+len(aka.RES)+3)/4))
	resAttr = append(resAttr, aka.RES...)
	for len(resAttr)%4 != 0 {
		resAttr = append(resAttr, 0)
	}

	// AT_MAC attribute (type 0x0B, length 5 words = 20 bytes): the value is
	// 18 bytes (16-byte MAC + 2 padding), zeroed initially.
	macAttr := make([]byte, 18)
	macAttr[0] = 0x0B
	macAttr[1] = 5

	body := append(append([]byte{}, resAttr...), macAttr...)

	resp := &eap.EAPPacket{
		Code:       eap.CodeResponse,
		Identifier: req.Identifier,
		Type:       eapType,
		SubType:    eap.SubtypeAKAChallenge,
		Data:       body,
	}

	// Compute the MAC over the response with AT_MAC zeroed.
	mac, err := computeEAPMAC(resp, aka.CK, aka.IK, eapType)
	if err != nil {
		return nil, err
	}
	// Place the 16-byte MAC into the AT_MAC value (offset 2 of the attribute).
	copy(body[len(resAttr)+2:], mac)
	return resp, nil
}

// computeEAPMAC computes the EAP-AKA MAC (RFC 4187 §5.2): the MAC is computed
// over the EAP packet with the AT_MAC attribute value zeroed, keyed by
// K_aut = prf(CK | IK, "EAP-AKA"). The result is the 16-byte HMAC-SHA1-128.
func computeEAPMAC(pkt *eap.EAPPacket, ck, ik []byte, eapType byte) ([]byte, error) {
	raw := pkt.Encode()
	// Zero the AT_MAC attribute (the last 20 bytes of the AKA body).
	if len(raw) >= 20 {
		for i := len(raw) - 20; i < len(raw); i++ {
			raw[i] = 0
		}
	}
	// K_aut = prf(CK | IK, "EAP-AKA" or "EAP-AKA'").
	key := append(append([]byte{}, ck...), ik...)
	label := "EAP-AKA"
	if eapType == eap.TypeAKAPrime {
		label = "EAP-AKA'"
	}
	mac := hmac.New(sha1.New, key)
	mac.Write([]byte(label))
	mac.Write(raw)
	return mac.Sum(nil)[:16], nil
}

// verifyEAPAKAMAC verifies the AT_MAC in an EAP-AKA challenge.
func verifyEAPAKAMAC(pkt *eap.EAPPacket, attrs map[byte]*eap.EAPAttribute, ck, ik []byte, eapType byte) error {
	macAttr := attrs[eap.AttrATMAC]
	if macAttr == nil {
		return errors.New("swu: AKA challenge missing AT_MAC")
	}
	if len(macAttr.Value) < 16 {
		return errors.New("swu: AT_MAC value too short")
	}
	expected := macAttr.Value[:16]
	// Recompute the MAC over the request with AT_MAC zeroed.
	raw := pkt.Encode()
	if len(raw) >= 20 {
		for i := len(raw) - 20; i < len(raw); i++ {
			raw[i] = 0
		}
	}
	key := append(append([]byte{}, ck...), ik...)
	label := "EAP-AKA"
	if eapType == eap.TypeAKAPrime {
		label = "EAP-AKA'"
	}
	mac := hmac.New(sha1.New, key)
	mac.Write([]byte(label))
	mac.Write(raw)
	got := mac.Sum(nil)[:16]
	if !hmac.Equal(got, expected) {
		return errors.New("swu: EAP-AKA MAC mismatch")
	}
	return nil
}

// verifyEAPReauthMAC verifies the MAC of an EAP-AKA fast re-authentication
// request (RFC 4187 §5.2).
func verifyEAPReauthMAC(pkt *eap.EAPPacket, attrs map[byte]*eap.EAPAttribute, mk []byte) error {
	macAttr := attrs[eap.AttrATMAC]
	if macAttr == nil {
		return errors.New("swu: reauth missing AT_MAC")
	}
	if len(macAttr.Value) < 16 {
		return errors.New("swu: AT_MAC value too short")
	}
	expected := macAttr.Value[:16]
	raw := pkt.Encode()
	if len(raw) >= 20 {
		for i := len(raw) - 20; i < len(raw); i++ {
			raw[i] = 0
		}
	}
	mac := hmac.New(sha1.New, mk)
	mac.Write(raw)
	if !hmac.Equal(mac.Sum(nil)[:16], expected) {
		return errors.New("swu: EAP reauth MAC mismatch")
	}
	return nil
}

// buildEAPMACSelfCheckProof computes the EAP-AKA MAC self-check proof
// (RFC 4187 §5.2) used to validate the CK/IK before sending the response.
func buildEAPMACSelfCheckProof(ck, ik []byte, eapType byte) []byte {
	key := append(append([]byte{}, ck...), ik...)
	label := "EAP-AKA"
	if eapType == eap.TypeAKAPrime {
		label = "EAP-AKA'"
	}
	mac := hmac.New(sha1.New, key)
	mac.Write([]byte(label))
	mac.Write([]byte("self-check"))
	return mac.Sum(nil)
}

// eapAttrDigest computes a digest over the EAP-AKA attributes for logging.
func eapAttrDigest(attrs map[byte]*eap.EAPAttribute) string {
	var b []byte
	for _, a := range attrs {
		b = append(b, a.Type)
	}
	h := sha256.Sum256(b)
	return fmt.Sprintf("%x", h[:4])
}

// validateKnownSimakaAttributes checks that the AKA challenge carries the
// expected attributes.
func validateKnownSimakaAttributes(attrs map[byte]*eap.EAPAttribute) error {
	if attrs[eap.AttrATRAND] == nil {
		return errors.New("swu: AKA challenge missing AT_RAND")
	}
	if attrs[eap.AttrATAUTN] == nil {
		return errors.New("swu: AKA challenge missing AT_AUTN")
	}
	return nil
}

// appendAKAChallengeMetaAttrs appends the AT_ANY_ID_REQ / AT_FULLAUTH_ID_REQ
// handling metadata to the AKA challenge attributes (recovered from the
// decompiled state_auth.go).
func (s *Session) appendAKAChallengeMetaAttrs(attrs map[byte]*eap.EAPAttribute) {
	// The decompiled implementation records whether the challenge requested a
	// permanent identity; the session stores it for the response.
	if attrs[eap.AttrATPermanentIDReq] != nil {
		s.mu.Lock()
		s.eapType = eap.TypeAKA
		s.mu.Unlock()
	}
}

// calcAKACheckcodeWithPending computes the AKA checkcode with the pending
// challenge state (recovered from the decompiled calcAKACheckcodeWithPending).
func (s *Session) calcAKACheckcodeWithPending(ck, ik []byte) []byte {
	return buildEAPMACSelfCheckProof(ck, ik, s.eapType)
}

// resolveATCheckcodeValue resolves the AT_CHECKCODE value for the response.
func (s *Session) resolveATCheckcodeValue(ck, ik []byte) []byte {
	return s.calcAKACheckcodeWithPending(ck, ik)
}

// md5Hex returns the hex MD5 of b (used by the digest-AKA path).
func md5Hex(b []byte) string {
	h := md5.Sum(b)
	return fmt.Sprintf("%x", h)
}

// binary helpers used by the AKA attribute encoding.
func putUint16(b []byte, v uint16) { binary.BigEndian.PutUint16(b, v) }
