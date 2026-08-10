package imscore

import (
	"context"
	"errors"
	"net"
	"strings"
	"time"

	"github.com/iniwex5/vowifi-go/internal/vowifi/imsendpoint"
	"github.com/iniwex5/vowifi-go/internal/vowifi/logging"
	"github.com/iniwex5/vowifi-go/internal/vowifi/sipkit"
)

// Status restores the v1.5.5 map status API.
func (s *Service) Status() map[string]interface{} {
	return s.captureStatusSnapshot().ToMap()
}

// StatusSnapshot restores the v1.5.5 value snapshot API.
func (s *Service) StatusSnapshot() ServiceStatus {
	return s.captureStatusSnapshot()
}

// Session returns a detached snapshot of the registered signaling session.
func (s *Service) Session() *imsendpoint.Session {
	if s == nil || s.cfg == nil {
		return nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	localPortC, _ := s.localPortsLocked()
	remoteIP, _, remotePortS := s.remoteEndpointLocked()
	serviceRoute := currentServiceRoute(s.regSession)
	return &imsendpoint.Session{
		SignalingConn: s.registrationTCP, LocalIP: s.cfg.LocalIP.String(),
		LocalPortC: localPortC, RemoteIP: remoteIP, RemotePortS: remotePortS,
		TransportMode: s.registrationTransport, ServiceRoute: serviceRoute,
		Path: s.path, SecVerify: s.securityVerify, SecMode: s.effectiveSecurityModeLocked(),
		RouteSet: splitSIPHeaderValues(serviceRoute), IMPU: s.cfg.IMPU, IMPI: s.cfg.IMPI,
		Domain: s.cfg.Domain, Realm: s.GetRealm(), MSISDN: s.assocMSISDN,
		Registered: s.regState == regRegistered || s.regStatus.Load() == registrationRegistered,
	}
}

// Snapshot restores the endpoint snapshot consumed by the original voice API.
func (s *Service) Snapshot() imsendpoint.Snapshot {
	runtime := s.GetIMSContextSnapshot()
	return imsendpoint.Snapshot{
		IMPU: runtime.IMPU, Realm: runtime.Realm, ContactID: runtime.ContactID,
		ServiceRoute: runtime.ServiceRoute, SecVerify: runtime.SecVerify,
		EffectiveSecMode:   runtime.EffectiveSecMode,
		PAccessNetworkInfo: runtime.PAccessNetworkInfo, UserAgent: runtime.UserAgent,
		LocalAddr: runtime.LocalAddr, LocalPortC: runtime.LocalPortC, LocalPortS: runtime.LocalPortS,
		RemotePortC: runtime.RemotePortC, RemotePortS: runtime.RemotePortS,
		LocalSpiC: runtime.LocalSpiC, LocalSpiS: runtime.LocalSpiS,
		RemoteSpiC: runtime.RemoteSpiC, RemoteSpiS: runtime.RemoteSpiS,
		Transport: runtime.Transport, IMEI: s.GetIMEI(), PubGRUU: s.GetPubGRUU(),
		Voice: s.VoiceProfile(),
	}
}

// GetIMSContextSnapshot restores the immutable SIP request-building context.
func (s *Service) GetIMSContextSnapshot() sipkit.IMSRuntimeSnapshot {
	if s == nil || s.cfg == nil {
		return sipkit.IMSRuntimeSnapshot{}
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	localPortC, localPortS := s.localPortsLocked()
	_, remotePortC, remotePortS := s.remoteEndpointLocked()
	localSpiC, localSpiS, remoteSpiC, remoteSpiS := s.spiPairsLocked()
	contactID := ""
	if s.regSession != nil {
		contactID = s.regSession.contactUser
	}
	localAddr := strings.TrimSpace(s.cfg.LocalAddr)
	if localAddr == "" {
		localAddr = s.cfg.LocalIP.String()
	}
	impu := strings.TrimSpace(s.cfg.IMPU)
	if s.regSession != nil && strings.TrimSpace(s.regSession.publicID) != "" {
		impu = strings.TrimSpace(s.regSession.publicID)
	}
	if impu == "" && strings.TrimSpace(s.assocMSISDN) != "" {
		domain := firstNonBlank(s.cfg.Realm, s.cfg.Domain)
		impu = "sip:" + strings.TrimSpace(s.assocMSISDN) + "@" + domain
	}
	return sipkit.IMSRuntimeSnapshot{
		IMPU: impu, Realm: firstNonBlank(s.cfg.Realm, s.cfg.Domain), ContactID: contactID,
		ServiceRoute: currentServiceRoute(s.regSession), SecVerify: s.securityVerify,
		EffectiveSecMode: s.effectiveSecurityModeLocked(), PAccessNetworkInfo: s.GetPAccessNetworkInfo(),
		UserAgent: s.cfg.UserAgent, LocalAddr: localAddr,
		LocalPortC: localPortC, LocalPortS: localPortS,
		RemotePortC: remotePortC, RemotePortS: remotePortS,
		LocalSpiC: localSpiC, LocalSpiS: localSpiS,
		RemoteSpiC: remoteSpiC, RemoteSpiS: remoteSpiS, Transport: s.registrationTransport,
	}
}

// GetIMPU returns the primary public identity from the original scalar field.
func (s *Service) GetIMPU() string {
	if s == nil || s.cfg == nil {
		return ""
	}
	return s.cfg.IMPU
}

// GetLocalPorts returns the protected client/server SIP ports.
func (s *Service) GetLocalPorts() (int, int) {
	if s == nil {
		return 0, 0
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.localPortsLocked()
}

func (s *Service) localPortsLocked() (int, int) {
	client, server := s.protectedClientPort, s.protectedServerPort
	if s.regSession != nil && s.regSession.security != nil {
		client = int(s.regSession.security.client.PortC)
		server = int(s.regSession.security.client.PortS)
	}
	if client == 0 && s.cfg != nil {
		client = s.cfg.LocalPort
	}
	if server == 0 {
		server = client
	}
	return client, server
}

// GetRemotePorts returns the negotiated remote client/server SIP ports.
func (s *Service) GetRemotePorts() (int, int) {
	if s == nil {
		return 0, 0
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	_, client, server := s.remoteEndpointLocked()
	return client, server
}

func (s *Service) remoteEndpointLocked() (string, int, int) {
	remoteIP, client, server := "", 0, 0
	if s.registrationRemote != nil {
		remoteIP = s.registrationRemote.IP.String()
		client, server = s.registrationRemote.Port, s.registrationRemote.Port
	}
	if s.regSession != nil && s.regSession.security != nil && s.regSession.security.server != nil {
		client = int(s.regSession.security.server.PortC)
		server = int(s.regSession.security.server.PortS)
	}
	return remoteIP, client, server
}

// GetServiceRoute returns the negotiated Service-Route header value.
func (s *Service) GetServiceRoute() string {
	if s == nil {
		return ""
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return currentServiceRoute(s.regSession)
}

// GetSpiPairs returns local client/server and remote client/server SPIs.
func (s *Service) GetSpiPairs() (uint32, uint32, uint32, uint32) {
	if s == nil {
		return 0, 0, 0, 0
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.spiPairsLocked()
}

func (s *Service) spiPairsLocked() (uint32, uint32, uint32, uint32) {
	if s.regSession == nil || s.regSession.security == nil || s.regSession.security.server == nil {
		return 0, 0, 0, 0
	}
	client, server := s.regSession.security.client, s.regSession.security.server
	return client.SPIC, client.SPIS, server.SPIC, server.SPIS
}

// ListenPacket restores the context-aware endpoint packet listener.
func (s *Service) ListenPacket(ctx context.Context, network string, address net.Addr) (net.PacketConn, error) {
	if s == nil || s.cfg == nil || s.cfg.IMSNetwork == nil {
		return nil, errors.New("imscore: no network")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	udpAddress, ok := address.(*net.UDPAddr)
	if !ok || udpAddress == nil {
		return nil, errors.New("imscore: packet listen address must be UDP")
	}
	return s.cfg.IMSNetwork.ListenPacket(network, udpAddress)
}

// Stop restores the context-bearing v1.5.5 lifecycle API.
func (s *Service) Stop(ctx context.Context) error {
	var contextErr error
	if ctx != nil {
		contextErr = ctx.Err()
	}
	s.StopCurrent()
	return contextErr
}

// TriggerRegisterImmediate schedules production maintenance to REGISTER now.
func (s *Service) TriggerRegisterImmediate(reason string) {
	s.triggerRegisterImmediate(reason)
}

// TriggerFastReconnect requests a runtime rebuild once for the active failure.
func (s *Service) TriggerFastReconnect(reason string) {
	if s == nil || s.stopped() {
		return
	}
	s.mu.Lock()
	s.registrationRefreshAt = time.Time{}
	s.nextRegister = time.Time{}
	s.regState = regFailed
	s.pingFailCount.Store(0)
	s.mu.Unlock()
	s.transitionRegStatus(registrationRejectedTemporary)
	logging.Info("IMS fast reconnect requested", "device", s.DeviceID(), "reason", strings.TrimSpace(reason))
	s.triggerRegisterReconnect()
}

// UpdateLastPingAt records successful signaling traffic at the current time.
func (s *Service) UpdateLastPingAt() {
	s.UpdateLastPingAtTime(time.Now())
}

// VoiceProfile returns the normalized carrier voice header policy.
func (s *Service) VoiceProfile() imsendpoint.VoiceProfile {
	if s == nil || s.cfg == nil {
		return imsendpoint.VoiceProfile{}
	}
	template := s.cfg.IMSRegisterTemplate
	return imsendpoint.VoiceProfile{
		SupportedHeader:   firstNonBlank(template.VoiceSupportedHeader, template.SupportedHeader),
		AllowHeader:       firstNonBlank(template.VoiceAllowHeader, template.AllowHeader),
		AcceptContact:     template.VoiceAcceptContact,
		PPreferredService: template.VoicePPreferredService,
		AccessType:        template.AccessType,
		ContactParamOrder: append([]string(nil), template.ContactParamOrder...),
	}
}
