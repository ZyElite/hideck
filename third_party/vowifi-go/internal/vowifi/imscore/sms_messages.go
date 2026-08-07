package imscore

import (
	"errors"
	"fmt"
	"net"
	"strings"
)

func (s *Service) buildSMSMESSAGE(remoteURI string, body []byte) (string, error) {
	if s == nil || s.cfg == nil {
		return "", errors.New("imscore: SMS service is not configured")
	}
	remoteURI = strings.TrimSpace(remoteURI)
	if remoteURI == "" || strings.ContainsAny(remoteURI, "\r\n") {
		return "", errors.New("imscore: invalid SMS remote URI")
	}
	localURI := primaryPublicIdentity(s.cfg)
	if localURI == "" {
		return "", errors.New("imscore: SMS local identity is unavailable")
	}
	clientAddress, serverAddress, route, securityVerify, transport := s.smsMessageRoute()
	if clientAddress == "" || serverAddress == "" {
		return "", errors.New("imscore: protected SMS transport is unavailable")
	}
	branch := "z9hG4bK" + newBranch()
	callID := newCallID()
	var request strings.Builder
	fmt.Fprintf(&request, "MESSAGE %s SIP/2.0\r\n", remoteURI)
	fmt.Fprintf(&request, "Via: SIP/2.0/%s %s;rport;branch=%s\r\n", transportUpper(transport), clientAddress, branch)
	if route != "" {
		request.WriteString("Route: " + route + "\r\n")
	}
	fmt.Fprintf(&request, "From: <%s>;tag=%s\r\n", localURI, newTag())
	fmt.Fprintf(&request, "To: <%s>\r\n", remoteURI)
	fmt.Fprintf(&request, "Call-ID: %s\r\nCSeq: 1 MESSAGE\r\n", callID)
	fmt.Fprintf(&request, "Contact: <sip:%s@%s;transport=%s>\r\n", contactUser(s.cfg), serverAddress, transport)
	request.WriteString("Max-Forwards: 70\r\n")
	request.WriteString("P-Preferred-Identity: <" + localURI + ">\r\n")
	request.WriteString("P-Preferred-Service: urn:urn-7:3gpp-service.ims.icsi.sms\r\n")
	request.WriteString("Accept-Contact: *;+g.3gpp.smsip\r\n")
	if securityVerify != "" {
		request.WriteString("Security-Verify: " + securityVerify + "\r\n")
	}
	if pani := strings.TrimSpace(s.GetPAccessNetworkInfo()); pani != "" {
		request.WriteString("P-Access-Network-Info: " + pani + "\r\n")
	}
	if userAgent := strings.TrimSpace(s.cfg.UserAgent); userAgent != "" {
		request.WriteString("User-Agent: " + userAgent + "\r\n")
	}
	request.WriteString("Content-Type: " + imsSMSContentType + "\r\n")
	request.WriteString("Content-Transfer-Encoding: binary\r\n")
	fmt.Fprintf(&request, "Content-Length: %d\r\n\r\n", len(body))
	request.Write(body)
	return request.String(), nil
}

func (s *Service) smsMessageRoute() (clientAddress, serverAddress, route, securityVerify, transport string) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	clientPort := s.protectedClientPort
	serverPort := s.protectedServerPort
	transport = "tcp"
	if s.registrationTCP == nil {
		clientPort = s.cfg.LocalPort
		serverPort = s.cfg.LocalPort
		transport = strings.ToLower(strings.TrimSpace(s.cfg.Transport))
		if transport == "" {
			transport = "udp"
		}
	}
	if clientPort > 0 {
		clientAddress = net.JoinHostPort(s.cfg.LocalIP.String(), fmt.Sprint(clientPort))
	}
	if serverPort > 0 {
		serverAddress = net.JoinHostPort(s.cfg.LocalIP.String(), fmt.Sprint(serverPort))
	}
	if s.regSession != nil {
		route = strings.TrimSpace(s.regSession.serviceRoute)
		if s.regSession.security != nil {
			securityVerify = strings.TrimSpace(s.regSession.security.verifyHeader)
		}
	}
	return clientAddress, serverAddress, route, securityVerify, transport
}
