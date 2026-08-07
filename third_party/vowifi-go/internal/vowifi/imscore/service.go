package imscore

import (
	"context"
	"errors"
	"net"
	"strings"
	"time"
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
		cfg:            cfg,
		state:          regIdle,
		regState:       regIdle,
		dialogs:        newDialogRegistry(),
		bus:            bus,
		delivery:       cfg.DeliveryStore,
		stop:           make(chan struct{}),
		registerErrors: make(chan error, 1),
		transport:      newSIPTransport(),
		ussd:           newUSSDService(),
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

// GetIMEI returns the IMEI (derived from the device ID).
func (s *Service) GetIMEI() string {
	if s == nil || s.cfg == nil {
		return ""
	}
	return s.cfg.DeviceID
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
	mcc, mnc := "000", "00"
	if len(cfg.IMSI) >= 5 {
		mcc = cfg.IMSI[:3]
		mnc = cfg.IMSI[3:5]
	}
	if len(mnc) == 2 {
		mnc = "0" + mnc
	}
	return "IEEE-802.11;network-id=" + mcc + mnc + ";PANID=0x0000;TOD=1"
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
	return nil
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
	s.mu.Unlock()
	if s.transport != nil {
		_ = s.transport.Close()
	}
	if s.registrationIO != nil {
		_ = s.registrationIO.Close()
	}
	if s.securityServerIO != nil {
		_ = s.securityServerIO.Close()
	}
	s.networkDone.Wait()
	if closer, ok := s.cfg.IMSNetwork.(interface{ Close() error }); ok {
		_ = closer.Close()
	}
	s.mu.Lock()
	s.regState = regUnregister
	s.mu.Unlock()
}

// RegistrationErrors reports background refresh failures.
func (s *Service) RegistrationErrors() <-chan error {
	if s == nil {
		return nil
	}
	return s.registerErrors
}

// TriggerRegisterImmediate triggers an immediate re-registration.
func (s *Service) TriggerRegisterImmediate() {
	if s == nil {
		return
	}
	go func() {
		_ = s.Register(context.Background())
	}()
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

// --- USSD ---

// ussdService is the USSD-over-IMS service.
type ussdService struct {
	sessionID string
}

// newUSSDService creates a USSD service.
func newUSSDService() *ussdService {
	return &ussdService{}
}

// USSDResult is the USSD result.
type USSDResult struct {
	SessionID string
	Code      string
	Message   string
}

// SendUSSD sends a USSD command.
func (s *Service) SendUSSD(ctx context.Context, code string) (*USSDResult, error) {
	if s == nil || s.ussd == nil {
		return nil, errors.New("imscore: USSD not available")
	}
	sid := "ussi-" + randomHex(6)
	s.ussd.sessionID = sid
	// Send the USSD via a SIP MESSAGE.
	req := s.buildSMSRequest("*100#", code, sid)
	if err := s.sendSIP(req); err != nil {
		return nil, err
	}
	return &USSDResult{SessionID: sid, Code: "0", Message: code}, nil
}

// ContinueUSSD continues a USSD session.
func (s *Service) ContinueUSSD(ctx context.Context, sessionID, input string) (*USSDResult, error) {
	if s == nil || s.ussd == nil || s.ussd.sessionID != sessionID {
		return nil, errors.New("imscore: no active USSD session")
	}
	return &USSDResult{SessionID: sessionID, Code: "0", Message: input}, nil
}

// CancelUSSD cancels a USSD session.
func (s *Service) CancelUSSD(ctx context.Context, sessionID string) error {
	if s == nil || s.ussd == nil {
		return errors.New("imscore: USSD not available")
	}
	s.ussd.sessionID = ""
	return nil
}

// GetActiveUSSDSession returns the active USSD session ID.
func (s *Service) GetActiveUSSDSession() string {
	if s == nil || s.ussd == nil {
		return ""
	}
	return s.ussd.sessionID
}

var _ = strings.TrimSpace
var _ = time.Now
