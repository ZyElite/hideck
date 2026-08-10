package imscore

import (
	"errors"
	"fmt"
	"net"
	"strings"

	"github.com/emiago/sipgo/sip"
)

const smsSupportedHeader = "path, 100rel, replaces, gruu"

func (s *Service) buildSMSMESSAGE(remoteURI string, body []byte) (string, error) {
	return s.buildSMSMESSAGEWithOptions(smsMESSAGEOptions{
		RemoteURI: remoteURI,
		Body:      body,
	})
}

type smsMESSAGEOptions struct {
	RemoteURI string
	Body      []byte
	InReplyTo string
}

func (s *Service) buildSMSMESSAGEWithOptions(options smsMESSAGEOptions) (string, error) {
	if s == nil || s.cfg == nil {
		return "", errors.New("imscore: SMS service is not configured")
	}
	remoteURI := strings.TrimSpace(options.RemoteURI)
	if remoteURI == "" || strings.ContainsAny(remoteURI, "\r\n") {
		return "", errors.New("imscore: invalid SMS remote URI")
	}
	inReplyTo := strings.TrimSpace(options.InReplyTo)
	if strings.ContainsAny(inReplyTo, "\r\n") {
		return "", errors.New("imscore: invalid SMS In-Reply-To")
	}
	profile, err := s.reserveRegisteredSIPProfile()
	if err != nil {
		return "", fmt.Errorf("imscore: SMS registered profile: %w", err)
	}
	branch := "z9hG4bK" + newBranch()
	callID := newCallID()
	var request strings.Builder
	fmt.Fprintf(&request, "MESSAGE %s SIP/2.0\r\n", remoteURI)
	fmt.Fprintf(&request, "Via: SIP/2.0/%s %s;rport;branch=%s\r\n", transportUpper(profile.Transport), profile.LocalAddress, branch)
	if profile.ServiceRoute != "" {
		request.WriteString("Route: " + profile.ServiceRoute + "\r\n")
	}
	fmt.Fprintf(&request, "From: <%s>;tag=%s\r\n", profile.LocalURI, newTag())
	fmt.Fprintf(&request, "To: <%s>\r\n", remoteURI)
	fmt.Fprintf(&request, "Call-ID: %s\r\n", callID)
	if inReplyTo != "" {
		request.WriteString("In-Reply-To: " + inReplyTo + "\r\n")
	}
	fmt.Fprintf(&request, "CSeq: %d MESSAGE\r\n", profile.InitialCSeq)
	request.WriteString("Contact: " + profile.ContactHeader + "\r\n")
	request.WriteString("Max-Forwards: 70\r\n")
	request.WriteString("P-Preferred-Identity: <" + profile.LocalURI + ">\r\n")
	request.WriteString("P-Preferred-Service: urn:urn-7:3gpp-service.ims.icsi.sms\r\n")
	request.WriteString("Accept-Contact: *;+g.3gpp.smsip\r\n")
	supported := smsSupportedHeader
	if profile.SecurityVerify != "" {
		supported += ", sec-agree"
		request.WriteString("Security-Verify: " + profile.SecurityVerify + "\r\n")
	}
	request.WriteString("Supported: " + supported + "\r\n")
	request.WriteString("Request-Disposition: no-fork\r\n")
	if pani := strings.TrimSpace(profile.PANI); pani != "" {
		request.WriteString("P-Access-Network-Info: " + pani + "\r\n")
	}
	if userAgent := strings.TrimSpace(profile.UserAgent); userAgent != "" {
		request.WriteString("User-Agent: " + userAgent + "\r\n")
	}
	request.WriteString("Content-Type: " + imsSMSContentType + "\r\n")
	request.WriteString("Content-Transfer-Encoding: binary\r\n")
	fmt.Fprintf(&request, "Content-Length: %d\r\n\r\n", len(options.Body))
	request.Write(options.Body)
	return request.String(), nil
}

func (s *Service) buildOutboundMESSAGE(remoteURI string, body []byte) (*sip.Request, error) {
	raw, err := s.buildSMSMESSAGE(remoteURI, body)
	if err != nil {
		return nil, err
	}
	message, err := parseSIPMessage(raw)
	if err != nil {
		return nil, fmt.Errorf("parse outbound MESSAGE: %w", err)
	}
	request, ok := message.(*sip.Request)
	if !ok {
		return nil, errors.New("outbound MESSAGE builder returned a non-request")
	}
	return request, nil
}

func (s *Service) buildRPAckMESSAGE(inbound string, body []byte) (*sip.Request, error) {
	raw, err := s.buildInboundSMSControlRequest(inbound, body)
	if err != nil {
		return nil, err
	}
	message, err := parseSIPMessage(raw)
	if err != nil {
		return nil, fmt.Errorf("parse RP-ACK MESSAGE: %w", err)
	}
	request, ok := message.(*sip.Request)
	if !ok {
		return nil, errors.New("RP-ACK builder returned a non-request")
	}
	return request, nil
}

type registeredSIPRoute struct {
	clientAddress  string
	serverAddress  string
	remoteAddress  string
	serviceRoute   string
	securityVerify string
	transport      string
	live           bool
}

func (s *Service) registeredSIPRouteLocked() registeredSIPRoute {
	clientPort := s.protectedClientPort
	serverPort := s.protectedServerPort
	transport := "tcp"
	if s.registrationTCP == nil {
		clientPort, serverPort = s.cfg.LocalPort, s.cfg.LocalPort
		transport = strings.ToLower(strings.TrimSpace(s.cfg.Transport))
		if transport == "" {
			transport = "udp"
		}
	}
	route := registeredSIPRoute{transport: transport}
	if clientPort > 0 {
		route.clientAddress = net.JoinHostPort(s.cfg.LocalIP.String(), fmt.Sprint(clientPort))
	}
	if serverPort > 0 {
		route.serverAddress = net.JoinHostPort(s.cfg.LocalIP.String(), fmt.Sprint(serverPort))
	}
	route.remoteAddress = s.registeredRemoteAddressLocked()
	if s.regSession != nil {
		route.serviceRoute = strings.TrimSpace(s.regSession.serviceRoute)
		if s.regSession.security != nil {
			route.securityVerify = strings.TrimSpace(s.regSession.security.verifyHeader)
		}
	}
	route.live = s.registeredSIPTransportReadyLocked()
	return route
}

func (s *Service) registeredRemoteAddressLocked() string {
	if s.registrationTCP != nil {
		if address := validSIPRemoteAddress(s.registrationTCP.RemoteAddr()); address != "" {
			return address
		}
	}
	if s.registrationRemote == nil || s.registrationRemote.IP == nil {
		return ""
	}
	port := s.registrationRemote.Port
	if s.regSession != nil && s.regSession.security != nil && s.regSession.security.server != nil {
		if negotiated := int(s.regSession.security.server.PortS); negotiated > 0 {
			port = negotiated
		}
	}
	if port < 1 {
		return ""
	}
	return net.JoinHostPort(s.registrationRemote.IP.String(), fmt.Sprint(port))
}

func validSIPRemoteAddress(address net.Addr) string {
	if address == nil {
		return ""
	}
	host, port, err := net.SplitHostPort(strings.TrimSpace(address.String()))
	if err != nil || strings.TrimSpace(host) == "" || strings.TrimSpace(port) == "" {
		return ""
	}
	return net.JoinHostPort(strings.Trim(host, "[]"), port)
}

func (s *Service) registeredSIPTransportReadyLocked() bool {
	if s.externalTransport {
		return true
	}
	if s.regSession != nil && s.regSession.security != nil &&
		strings.TrimSpace(s.regSession.security.verifyHeader) != "" {
		return s.registrationTCP != nil && s.registrationTCPProtected
	}
	return s.registrationTCP != nil || s.registrationIO != nil
}

func (s *Service) smsMessageRoute() (clientAddress, serverAddress, route, securityVerify, transport string) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	snapshot := s.registeredSIPRouteLocked()
	return snapshot.clientAddress, snapshot.serverAddress, snapshot.serviceRoute,
		snapshot.securityVerify, snapshot.transport
}
