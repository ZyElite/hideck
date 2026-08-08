package imscore

import (
	"errors"
	"fmt"
	"net"
	"strings"
)

const smsSupportedHeader = "path, 100rel, replaces, gruu"

func (s *Service) buildSMSMESSAGE(remoteURI string, body []byte) (string, error) {
	if s == nil || s.cfg == nil {
		return "", errors.New("imscore: SMS service is not configured")
	}
	remoteURI = strings.TrimSpace(remoteURI)
	if remoteURI == "" || strings.ContainsAny(remoteURI, "\r\n") {
		return "", errors.New("imscore: invalid SMS remote URI")
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
	fmt.Fprintf(&request, "Call-ID: %s\r\nCSeq: %d MESSAGE\r\n", callID, profile.InitialCSeq)
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
	fmt.Fprintf(&request, "Content-Length: %d\r\n\r\n", len(body))
	request.Write(body)
	return request.String(), nil
}

type registeredSIPRoute struct {
	clientAddress  string
	serverAddress  string
	serviceRoute   string
	securityVerify string
	transport      string
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
	if s.regSession != nil {
		route.serviceRoute = strings.TrimSpace(s.regSession.serviceRoute)
		if s.regSession.security != nil {
			route.securityVerify = strings.TrimSpace(s.regSession.security.verifyHeader)
		}
	}
	return route
}

func (s *Service) smsMessageRoute() (clientAddress, serverAddress, route, securityVerify, transport string) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	snapshot := s.registeredSIPRouteLocked()
	return snapshot.clientAddress, snapshot.serverAddress, snapshot.serviceRoute,
		snapshot.securityVerify, snapshot.transport
}
