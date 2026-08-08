package imscore

import (
	"context"
	"errors"
	"net"
	"strings"
	"time"

	"github.com/iniwex5/vowifi-go/internal/smscodec"
	"github.com/iniwex5/vowifi-go/internal/vowifi/ussi"
)

// New creates an IMS service from the configuration.
func New(cfg *IMSConfig) (*Service, error) {
	if cfg == nil {
		return nil, errors.New("imscore: nil config")
	}
	if cfg.IMSNetwork == nil {
		cfg.IMSNetwork = NewSystemIMSNetwork(cfg.LocalIP)
	}
	if cfg.Domain == "" {
		cfg.Domain = "ims.mnc000.mcc000.3gppnetwork.org"
	}
	bus := cfg.EventBus
	if bus == nil {
		bus = newIMSEventBus()
	}
	s := &Service{
		cfg:                   cfg,
		state:                 regIdle,
		regState:              regIdle,
		dialogs:               newDialogRegistry(),
		bus:                   bus,
		delivery:              cfg.DeliveryStore,
		stop:                  make(chan struct{}),
		registerErrors:        make(chan error, 1),
		protectedConns:        make(map[net.Conn]struct{}),
		transport:             newSIPTransport(),
		ussd:                  ussi.NewService(),
		smsReassembler:        smscodec.NewReassembler(),
		smsTransactionTimeout: outboundSMSTransactionTimeout,
		smsReportTimeout:      defaultSMSDeliveryReportTimeout,
		keepaliveInterval:     imsKeepaliveInterval,
		keepaliveTimeout:      imsKeepaliveTransactionTimeout,
		keepaliveFailureLimit: imsKeepaliveFailureLimit,
	}
	return s, nil
}

// SetOnRegistered wires the registration callback.
func (s *Service) SetOnRegistered(fn func()) {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.onRegistered = fn
	s.mu.Unlock()
}

// IsRegistered reports whether the service is registered.
func (s *Service) IsRegistered() bool {
	if s == nil {
		return false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.regState == regRegistered
}

// RegState returns the registration state.
func (s *Service) RegState() string {
	if s == nil {
		return regIdle
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.regState
}

// Status returns a status snapshot.
func (s *Service) Status() *ServiceStatus {
	if s == nil {
		return &ServiceStatus{}
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return &ServiceStatus{
		Registered: s.regState == regRegistered,
		State:      s.state,
		RegState:   s.regState,
		IMPU:       append([]string{}, s.cfg.IMPU...),
		Domain:     s.cfg.Domain,
	}
}

// StatusSnapshot returns a status snapshot.
func (s *Service) StatusSnapshot() *ServiceStatus {
	return s.Status()
}

// DeviceID returns the device ID.
func (s *Service) DeviceID() string {
	if s == nil || s.cfg == nil {
		return ""
	}
	return s.cfg.DeviceID
}

// GetIMSI returns the IMSI.
func (s *Service) GetIMSI() string {
	if s == nil || s.cfg == nil {
		return ""
	}
	return s.cfg.IMSI
}

// GetIMPU returns the IMPU list.
func (s *Service) GetIMPU() []string {
	if s == nil || s.cfg == nil {
		return nil
	}
	return append([]string{}, s.cfg.IMPU...)
}

// GetIMEI returns the configured mobile equipment identity.
func (s *Service) GetIMEI() string {
	if s == nil || s.cfg == nil {
		return ""
	}
	if strings.TrimSpace(s.cfg.IMEI) == "" {
		return s.cfg.DeviceID
	}
	return s.cfg.IMEI
}

// GetIMSServerAddr returns the IMS server address.
func (s *Service) GetIMSServerAddr() string {
	if s == nil || s.cfg == nil {
		return ""
	}
	return s.cfg.Registrar
}

// GetLocalIMSAddr returns the local IMS address.
func (s *Service) GetLocalIMSAddr() string {
	if s == nil || s.cfg == nil || s.cfg.LocalIP == nil {
		return ""
	}
	return formatHostPort(s.cfg.LocalIP)
}

// GetLocalPorts returns the local SIP ports.
func (s *Service) GetLocalPorts() []int {
	if s == nil || s.cfg == nil {
		return nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	ports := []int{s.cfg.LocalPort}
	if s.protectedClientPort > 0 && s.protectedClientPort != s.cfg.LocalPort {
		ports = append(ports, s.protectedClientPort)
	}
	if s.protectedServerPort > 0 && s.protectedServerPort != s.cfg.LocalPort {
		ports = append(ports, s.protectedServerPort)
	}
	return ports
}

// GetRemotePorts returns the remote SIP ports.
func (s *Service) GetRemotePorts() []int {
	if s == nil {
		return nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.regSession != nil && s.regSession.security != nil && s.regSession.security.server != nil {
		return []int{int(s.regSession.security.server.PortC), int(s.regSession.security.server.PortS)}
	}
	if s.registrationRemote != nil {
		return []int{s.registrationRemote.Port}
	}
	return nil
}

// GetPAccessNetworkInfo returns the P-Access-Network-Info header value.
func (s *Service) GetPAccessNetworkInfo() string {
	cfg := s.cfg
	if cfg == nil {
		return ""
	}
	impu := ""
	if len(cfg.IMPU) > 0 {
		impu = cfg.IMPU[0]
	}
	seed := stablePANIGenerationSeed([]string{cfg.IMSI, cfg.IMPI, impu, cfg.Domain, cfg.DeviceID})
	return AppendPAccessNetworkCountry(
		GenerateStablePAccessNetworkInfo(seed), cfg.PAccessNetworkCountry,
	)
}

// GetPubGRUU returns the public GRUU.
func (s *Service) GetPubGRUU() string {
	if s == nil || s.cfg == nil {
		return ""
	}
	return "sip:" + s.cfg.IMPI + "@" + s.cfg.Domain
}

// GetTempGRUU returns the temporary GRUU.
func (s *Service) GetTempGRUU() string {
	return ""
}

// GetRealm returns the digest realm.
func (s *Service) GetRealm() string {
	if s == nil || s.cfg == nil {
		return ""
	}
	if s.cfg.Realm != "" {
		return s.cfg.Realm
	}
	return s.cfg.Domain
}

// GetServiceRoute returns the service route.
func (s *Service) GetServiceRoute() []string {
	if s == nil {
		return nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.regSession == nil || strings.TrimSpace(s.regSession.serviceRoute) == "" {
		return nil
	}
	return splitSIPHeaderValues(s.regSession.serviceRoute)
}

// GetSpiPairs returns the IPsec SPI pairs.
func (s *Service) GetSpiPairs() [][2]uint32 {
	if s == nil {
		return nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.spiPairs
}

// GetSecurityVerify returns the Security-Verify header value.
func (s *Service) GetSecurityVerify() string {
	if s == nil {
		return ""
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.securityVerify
}

// GetIMSContextSnapshot returns a context snapshot.
func (s *Service) GetIMSContextSnapshot() map[string]interface{} {
	return map[string]interface{}{
		"registered": s.IsRegistered(),
		"reg_state":  s.RegState(),
		"impi":       s.cfg.IMPI,
		"domain":     s.cfg.Domain,
	}
}

// ListenPacket returns a UDP packet connection.
func (s *Service) ListenPacket(network string, addr *net.UDPAddr) (net.PacketConn, error) {
	if s == nil || s.cfg == nil || s.cfg.IMSNetwork == nil {
		return nil, errors.New("imscore: no network")
	}
	return s.cfg.IMSNetwork.ListenPacket(network, addr)
}

// Stop shuts the service down.
func (s *Service) Stop() {
	if s == nil {
		return
	}
	s.setSMSReceiverReady(false)
	if s.ussd != nil {
		s.ussd.Stop()
	}
	select {
	case <-s.stop:
	default:
		close(s.stop)
	}
	s.mu.Lock()
	if s.refreshTimer != nil {
		s.refreshTimer.Stop()
		s.refreshTimer = nil
	}
	registrationIO := s.registrationIO
	registrationTCP := s.registrationTCP
	registrationPreviousTCP := s.registrationPreviousTCP
	securityServerIO := s.securityServerIO
	clientPortReserve := s.clientPortReserve
	s.registrationIO = nil
	s.registrationTCP = nil
	s.registrationPreviousTCP = nil
	s.registrationTCPProtected = false
	s.registrationTransport = ""
	s.securityServerIO = nil
	s.clientPortReserve = nil
	s.mu.Unlock()
	if s.transport != nil {
		_ = s.transport.Close()
	}
	if registrationIO != nil {
		_ = registrationIO.Close()
	}
	if registrationTCP != nil {
		_ = registrationTCP.Close()
	}
	if registrationPreviousTCP != nil && registrationPreviousTCP != registrationTCP {
		_ = registrationPreviousTCP.Close()
	}
	if securityServerIO != nil {
		_ = securityServerIO.Close()
	}
	if clientPortReserve != nil {
		_ = clientPortReserve.Close()
	}
	s.protectedConnMu.Lock()
	for conn := range s.protectedConns {
		_ = conn.Close()
	}
	s.protectedConnMu.Unlock()
	s.networkDone.Wait()
	if closer, ok := s.cfg.IMSNetwork.(interface{ Close() error }); ok {
		_ = closer.Close()
	}
	s.mu.Lock()
	s.regState = regUnregister
	s.mu.Unlock()
	s.notifySMSReadiness()
}

// RegistrationErrors reports background refresh failures.
func (s *Service) RegistrationErrors() <-chan error {
	if s == nil {
		return nil
	}
	return s.registerErrors
}

// TriggerRegisterImmediate performs an immediate re-registration and exposes
// the real result to its caller.
func (s *Service) TriggerRegisterImmediate() error {
	if s == nil {
		return errors.New("imscore: nil service")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	return s.Register(ctx)
}

// Unregister deregisters from the IMS network.
func (s *Service) Unregister(ctx context.Context) error {
	if s == nil || s.regSession == nil {
		return nil
	}
	s.regSession.cseq++
	req := s.buildRegister(s.regSession, "")
	if err := s.sendSIP(req); err != nil {
		return err
	}
	s.mu.Lock()
	s.regState = regUnregister
	s.mu.Unlock()
	return nil
}
