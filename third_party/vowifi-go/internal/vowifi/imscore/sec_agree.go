package imscore

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/iniwex5/vowifi-go/internal/vowifi/ipsec3gpp"
	"github.com/iniwex5/vowifi-go/internal/vowifi/logging"
	"github.com/iniwex5/vowifi-go/internal/vowifi/policy"
)

type securityMechanism struct {
	Raw                    string
	Name, Auth, Encryption string
	Protocol, Mode         string
	SPIC, SPIS             uint32
	PortC, PortS           uint16
	Priority               float64
}

type securityAgreement struct {
	client       securityMechanism
	server       *securityMechanism
	clientHeader string
	verifyHeader string
}

func (s *Service) prepareSecurityAgreement(template policy.IMSRegisterTemplate) (*securityAgreement, error) {
	if effectiveSecAgreeMode(s.cfg, template) == "disabled" {
		return nil, nil
	}
	s.mu.RLock()
	clientPort, serverPort := s.protectedClientPort, s.protectedServerPort
	if clientPort == 0 && s.externalTransport {
		clientPort = s.cfg.LocalPort
	}
	s.mu.RUnlock()
	if clientPort <= 0 || clientPort > 65535 || serverPort <= 0 || serverPort > 65535 {
		return nil, errors.New("imscore: protected IMS ports were not bound")
	}
	spiC, err := randomUint32(nil)
	if err != nil {
		return nil, err
	}
	spiS, err := randomUint32(map[uint32]struct{}{spiC: {}})
	if err != nil {
		return nil, err
	}
	client := securityMechanism{
		Name: "ipsec-3gpp", Auth: ipsec3gpp.AuthHMACSHA196,
		Encryption: ipsec3gpp.EncryptionAES, Protocol: ipsec3gpp.ProtocolESP, Mode: ipsec3gpp.ModeTransport,
		SPIC: spiC, SPIS: spiS, PortC: uint16(clientPort), PortS: uint16(serverPort),
	}
	clientHeader := buildTemplateSecurityClient(client, template)
	if err := validateSecAgreeRegisterParams(true, clientHeader); err != nil {
		if isMissingSecurityClientForSecAgree(err) {
			return nil, fmt.Errorf("imscore: invalid sec-agree REGISTER parameters: %w", err)
		}
		return nil, err
	}
	return &securityAgreement{client: client, clientHeader: clientHeader}, nil
}

func randomUint32(excluded map[uint32]struct{}) (uint32, error) {
	var encoded [4]byte
	for attempt := 0; attempt < 8; attempt++ {
		if _, err := rand.Read(encoded[:]); err != nil {
			return 0, fmt.Errorf("imscore: generate IPsec SPI: %w", err)
		}
		value := binary.BigEndian.Uint32(encoded[:])
		if value == 0 {
			continue
		}
		if _, exists := excluded[value]; !exists {
			return value, nil
		}
	}
	return 0, errors.New("imscore: failed to generate a unique IPsec SPI")
}

func selectSecurityServer(header string) (*securityMechanism, string, error) {
	return selectSecurityServerOfferForTemplate(header, policy.DefaultIMSRegisterTemplate())
}

func splitSecurityMechanisms(header string) []string {
	var values []string
	start := 0
	quoted := false
	for index, character := range header {
		switch character {
		case '"':
			quoted = !quoted
		case ',':
			if !quoted {
				values = append(values, strings.TrimSpace(header[start:index]))
				start = index + 1
			}
		}
	}
	return append(values, strings.TrimSpace(header[start:]))
}

func parseSecurityMechanism(value string) (securityMechanism, error) {
	parts := strings.Split(value, ";")
	mechanism := securityMechanism{Raw: strings.TrimSpace(value), Name: strings.ToLower(strings.TrimSpace(parts[0]))}
	parameters := make(map[string]string, len(parts)-1)
	for _, part := range parts[1:] {
		key, parameter, ok := strings.Cut(strings.TrimSpace(part), "=")
		if !ok || strings.TrimSpace(key) == "" {
			return securityMechanism{}, errors.New("imscore: malformed Security-Server parameter")
		}
		key = strings.ToLower(strings.TrimSpace(key))
		if _, duplicate := parameters[key]; duplicate {
			return securityMechanism{}, fmt.Errorf("imscore: duplicate Security-Server parameter %s", key)
		}
		parameters[key] = strings.Trim(strings.TrimSpace(parameter), "\"")
	}
	if err := assignSecurityParameters(&mechanism, parameters); err != nil {
		return securityMechanism{}, err
	}
	return mechanism, nil
}

func assignSecurityParameters(mechanism *securityMechanism, parameters map[string]string) error {
	mechanism.Auth = strings.ToLower(parameters["alg"])
	mechanism.Encryption = strings.ToLower(parameters["ealg"])
	mechanism.Protocol = defaultSecurityParameter(parameters["prot"], ipsec3gpp.ProtocolESP)
	mechanism.Mode = defaultSecurityParameter(parameters["mod"], ipsec3gpp.ModeTransport)
	if mechanism.Encryption == "" {
		mechanism.Encryption = ipsec3gpp.EncryptionNull
	}
	mechanism.Priority = 1
	if value := strings.TrimSpace(parameters["q"]); value != "" {
		priority, err := strconv.ParseFloat(value, 64)
		if err != nil || priority < 0 || priority > 1 {
			return errors.New("imscore: invalid Security-Server q value")
		}
		mechanism.Priority = priority
	}
	var err error
	if mechanism.SPIC, err = parseSPI(parameters["spi-c"]); err != nil {
		return fmt.Errorf("imscore: invalid Security-Server spi-c: %w", err)
	}
	if mechanism.SPIS, err = parseSPI(parameters["spi-s"]); err != nil {
		return fmt.Errorf("imscore: invalid Security-Server spi-s: %w", err)
	}
	if mechanism.PortC, err = parsePort(parameters["port-c"]); err != nil {
		return fmt.Errorf("imscore: invalid Security-Server port-c: %w", err)
	}
	if mechanism.PortS, err = parsePort(parameters["port-s"]); err != nil {
		return fmt.Errorf("imscore: invalid Security-Server port-s: %w", err)
	}
	return nil
}

func defaultSecurityParameter(value, fallback string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return fallback
	}
	return value
}

func parseSPI(value string) (uint32, error) {
	parsed, err := strconv.ParseUint(strings.TrimSpace(value), 10, 32)
	if err != nil || parsed == 0 {
		return 0, errors.New("non-zero decimal SPI required")
	}
	return uint32(parsed), nil
}

func parsePort(value string) (uint16, error) {
	parsed, err := strconv.ParseUint(strings.TrimSpace(value), 10, 16)
	if err != nil || parsed == 0 {
		return 0, errors.New("non-zero decimal port required")
	}
	return uint16(parsed), nil
}

func mechanismSupported(mechanism securityMechanism) bool {
	if mechanism.Name != "ipsec-3gpp" ||
		(mechanism.Auth != ipsec3gpp.AuthHMACSHA196 && mechanism.Auth != "hmac-md5-96") {
		return false
	}
	if mechanism.Protocol != ipsec3gpp.ProtocolESP || mechanism.Mode != ipsec3gpp.ModeTransport {
		return false
	}
	return mechanism.Encryption == ipsec3gpp.EncryptionAES || mechanism.Encryption == ipsec3gpp.Encryption3DES ||
		mechanism.Encryption == ipsec3gpp.EncryptionNull
}

func (s *Service) installNegotiatedIPSec(ctx context.Context, session *registerSession, response *sipResponse, aka AKAResult) error {
	challenge := s.evaluateRegisterSecurityChallenge(response, session.template)
	server, verify, decision := challenge.server, challenge.verify, challenge.decision
	if decision.err != nil {
		reason := classifySecurityFallbackReason(decision)
		s.recordSignalingFailure(decision.mode, reason, decision.err)
		return fmt.Errorf("imscore: %s: %w", reason, decision.err)
	}
	if !decision.useIPSec {
		if err := s.removeInstalledIPSec3GPP(); err != nil {
			s.recordSignalingFailure(decision.mode, decision.reason, err)
			return fmt.Errorf("imscore: remove negotiated 3GPP IPsec: %w", err)
		}
		s.releaseUnusedProtectedReservations()
		session.security = nil
		reason := classifySecurityFallbackReason(decision)
		s.recordSecurityMode(decision.mode, reason, reason == securityAutoFallback)
		return nil
	}
	remoteIP, err := s.selectNegotiatedIPSecRemote(ctx, session)
	if err != nil {
		s.recordSignalingFailure(decision.mode, decision.reason, err)
		return err
	}
	client := session.security.client
	policy := ipsec3gpp.Policy{
		LocalIP: s.cfg.LocalIP, RemoteIP: remoteIP,
		LocalPortC: int(client.PortC), LocalPortS: int(client.PortS),
		RemotePortC: int(server.PortC), RemotePortS: int(server.PortS),
		FlowC: ipsec3gpp.Flow{
			OutboundSPI: server.SPIS, InboundSPI: client.SPIC,
			LocalPort: int(client.PortC), RemotePort: int(server.PortS),
			AuthAlg: server.Auth, EncAlg: server.Encryption, CK: aka.CK, IK: aka.IK,
		},
		FlowS: ipsec3gpp.Flow{
			OutboundSPI: server.SPIC, InboundSPI: client.SPIS,
			LocalPort: int(client.PortS), RemotePort: int(server.PortC),
			AuthAlg: server.Auth, EncAlg: server.Encryption, CK: aka.CK, IK: aka.IK,
		},
	}
	if err := s.InstallIPSec3GPP(policy); err != nil {
		s.recordSignalingFailure(decision.mode, decision.reason, err)
		return fmt.Errorf("imscore: install negotiated 3GPP IPsec: %w", err)
	}
	logging.Info("IMS 3GPP IPsec policy installed",
		"auth", policy.FlowC.AuthAlg, "encryption", policy.FlowC.EncAlg,
		"local_client_port", policy.LocalPortC, "local_server_port", policy.LocalPortS,
		"remote_client_port", policy.RemotePortC, "remote_server_port", policy.RemotePortS)
	previousRemote := s.currentRegistrationRemote()
	if err := s.setProtectedRegistrarEndpoint(remoteIP, server.PortS); err != nil {
		return s.rollbackInstalledIPSec(err, previousRemote)
	}
	externalTransport, connected := s.protectedTransportState()
	if !externalTransport && !connected {
		if err := s.connectProtectedRegistrationTCP(ctx, client, *server); err != nil {
			return s.rollbackInstalledIPSec(err, previousRemote)
		}
	}
	s.recordSecurityAgreement(session, server, verify)
	s.recordSecurityMode(decision.mode, "", false)
	return nil
}

func (s *Service) recordSecurityAgreement(session *registerSession, server *securityMechanism, verify string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if session.security.server != nil && ipsec3gppOfferEqualForSA(*session.security.server, *server) {
		server = session.security.server
	}
	session.security.server = server
	session.security.verifyHeader = verify
	s.securityVerify = verify
	s.spiPairs = [][2]uint32{
		{session.security.client.SPIC, server.SPIS},
		{session.security.client.SPIS, server.SPIC},
	}
}
