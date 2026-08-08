package swu

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/iniwex5/vowifi-go/engine/crypto"
	"github.com/iniwex5/vowifi-go/engine/driver"
	engineeap "github.com/iniwex5/vowifi-go/engine/eap"
	"github.com/iniwex5/vowifi-go/engine/ikev2"
	"github.com/iniwex5/vowifi-go/engine/ipsec"
	enginesim "github.com/iniwex5/vowifi-go/engine/sim"
	"github.com/iniwex5/vowifi-go/engine/swu/eapaka"
)

// ErrFreshRuntimeRequired tells the host to replace the current IKE runtime.
var ErrFreshRuntimeRequired = errors.New("swu: full reauthentication requires a fresh runtime session")

// Config carries the SWu session configuration recovered from the decompiled
// engine/swu. It is the input to NewSession.
type Config struct {
	// DeviceID identifies the access device in transport diagnostics.
	DeviceID string
	// DNSServer optionally selects the resolver used for ePDG lookup.
	DNSServer string
	// EPDGAddr is the ePDG host (FQDN or IP) and optional port.
	EPDGAddr string
	// EpDGAddr/EpDGPort are the original endpoint fields.
	EpDGAddr string
	EpDGPort uint16
	// APN is carried as IDr in the first IKE_AUTH request (3GPP TS 24.302).
	// Empty requests the operator's default APN.
	APN string
	// LocalIP is the local address to bind the IKE/ESP socket to.
	LocalIP net.IP
	// LocalAddr/LocalPort retain the original bind-address API.
	LocalAddr string
	LocalPort uint16
	// ProxyAddr and Proxy route IKE/ESP through a SOCKS5 UDP associate.
	ProxyAddr string
	Proxy     *ipsec.Socks5Config
	// IMSI is the subscriber IMSI used for the EAP-AKA identity.
	IMSI string
	// MCC/MNC override the MCC/MNC derived from the IMSI in the NAI.
	MCC string
	MNC string
	// AKAProvider computes AKA from the network challenge (RAND, AUTN).
	AKAProvider AKAProvider
	// SIM and IPStackType retain the original configuration fields. AKAProvider
	// and IPStack are current aliases kept for source compatibility.
	SIM         AKAProvider
	IPStackType string
	IPStack     string
	// The following fields retain the original IKE_AUTH/EAP identity and
	// interoperability policy. DisableEAPMACValidation is an explicit unsafe
	// diagnostic switch and is never enabled by default.
	DisableEAPMACValidation   bool
	EnableDeviceIdentitySpoof bool
	DeviceIdentityIMEI        string
	IKEIdentityMode           string
	AKAChallengeMode          string
	AKAIdentityMode           string
	AKAPrimePreferred         bool
	// Fast reauthentication material can be restored by the runtime host.
	FastReauthID    string
	FastReauthMK    []byte
	FastReauthKAut  []byte
	FastReauthKEncr []byte
	// OnFastReauthUpdate persists a newly issued reauthentication identity.
	OnFastReauthUpdate func(reauthID string, mk, kAut, kEncr []byte)
	// AlgorithmPolicy selects the IKE/ESP algorithm offer policy.
	AlgorithmPolicy string
	// IKEProposals and ESPProposals are ordered legacy proposal strings. Empty
	// selects the original multi-proposal compatibility set.
	IKEProposals []string
	ESPProposals []string
	// Legacy encryption is disabled unless explicitly enabled and allowed.
	EnableLegacyCiphers  bool
	AllowedLegacyCiphers []string
	// IKEEncryption / IKEPRF / IKEIntegrity / IKEDH are the IKE algorithm
	// transform IDs (RFC 7296 §3.3.2). Zero selects the policy default.
	IKEEncryption        uint16
	IKEEncryptionKeyBits uint16
	IKEPRF               uint16
	IKEIntegrity         uint16
	IKEDH                uint16
	// ESPEncryption / ESPIntegrity are the ESP transform IDs for the CHILD_SA.
	ESPEncryption        uint16
	ESPEncryptionKeyBits uint16
	ESPIntegrity         uint16
	// DataplaneMode selects userspace, TUN, or Linux XFRM processing. Empty is
	// equivalent to userspace so current callers keep their existing behavior.
	DataplaneMode string
	TUNName       string
	TUNMTU        int
	XFRMIfID      uint32
	ReplayWindow  int
	EnableESN     bool
	// NonceLen is the initiator nonce length (default 32).
	NonceLen int
	// RekeyIKESeconds / RekeyChildSeconds drive the SA rekey timers.
	RekeyIKESeconds   time.Duration
	RekeyChildSeconds time.Duration
	ReauthSeconds     time.Duration
	NATKeepaliveEvery time.Duration
	DPDProbeEvery     time.Duration
	// Retransmit controls IKE request retries. Nil selects the recovered
	// RFC 7296 policy used by TaskManager.
	Retransmit *RetransmitConfig
	// Wireshark enables the pcap-like traffic logger.
	Wireshark bool
	// OnRedirect is invoked when the ePDG redirects the session (RFC 5685).
	OnRedirect func(target string)
	// OnStateChange is invoked on session state transitions.
	OnStateChange func(state string)
}

// AKAProvider computes AKA from the network challenge (RAND, AUTN).
type AKAProvider = enginesim.AKAProvider

// AKAResult is the outcome of an AKA computation.
type AKAResult = enginesim.AKAResult

// Session state strings (recovered from the decompiled status.go).
const (
	stateIdle           = "idle"
	stateConnecting     = "connecting"
	stateAuthenticating = "authenticating"
	stateEstablished    = "established"
	stateError          = "error"
	stateShutdown       = "shutdown"
)

// Data-plane modes recovered from the original session configuration.
const (
	DataplaneModeUserspace = "userspace"
	DataplaneModeTUN       = "tun"
	DataplaneModeXFRMI     = "xfrmi"
)

// ikeAuthStage tracks the IKE_AUTH exchange progress.
type ikeAuthStage int

const (
	stageInit  ikeAuthStage = iota // build & send IKE_AUTH request
	stageEAP                       // EAP exchange in progress
	stageFinal                     // final IKE_AUTH request (AUTH + EAP success)
	stageDone                      // IKE_AUTH complete
)

// Session is the SWu IKEv2 + EAP-AKA session. It extends the key-derivation
// fields in types.go with the transport, IKE_AUTH state, data plane and timers
// recovered from the decompiled engine/swu.
type Session struct {
	// --- IKE identifiers / negotiation (types.go) ---
	SPIi              [8]byte
	SPIr              [8]byte
	localIKEInitiator bool
	Ni                []byte
	nr                []byte // responder nonce (stored during IKE_SA_INIT)

	prf    crypto.PRF
	prfKey []byte

	integKeyLen    int
	encKeyLen      int
	encKeyBits     uint16
	aead           bool
	espEncKeyLen   int
	espEncKeyBits  uint16
	espIntegKeyLen int
	espAEAD        bool

	dhSharedSecret []byte
	ikeKeys        *IKEKeys

	dh       *crypto.DiffieHellman
	dhGroup  uint16
	encrAlg  uint16
	prfAlg   uint16
	integAlg uint16
	nonceLen int

	cookie []byte

	natSourceHash []byte
	natDestHash   []byte

	// --- configuration ---
	cfg *Config

	// --- transport ---
	socket ipsec.Transport

	// --- IKE_AUTH state ---
	stage                  ikeAuthStage
	eapID                  byte // current EAP identifier
	eapType                byte // negotiated EAP method (AKA / AKA')
	eapKeys                eapaka.Keys
	fastReauthCtx          *engineeap.FastReauthContext
	ikeIdentity            string
	eapIdentity            string
	eapIdentitySet         bool
	eapTranscript          [][]byte
	eapIdentityTranscript  [][]byte
	eapResultIndicated     bool
	eapResultConfirmed     bool
	authPayload            []byte // responder AUTH payload (for verification)
	skf                    []byte // SKF (encrypted IKE_AUTH response) pending decrypt
	responderAuthenticated bool
	eapOnlyAuthentication  bool
	eapOnlyRequested       bool
	responderIDType        byte
	responderID            []byte
	ikeSAInitRequest       []byte
	ikeSAInitResponse      []byte

	// --- data plane ---
	innerEndpoint   *userspaceInnerPacketEndpoint
	tun             *driver.TUNDevice
	networkTxn      *driver.NetTxn
	kernelDataPlane kernelDataPlane
	espOutboundSA   *ipsec.SecurityAssociation
	espInboundSA    *ipsec.SecurityAssociation
	espLocalSPI     uint32
	espRemoteSPI    uint32
	childNi         []byte
	childNr         []byte
	childTSi        *ikev2.EncryptedPayloadTS
	childTSr        *ikev2.EncryptedPayloadTS
	espCipher       uint16
	espInteg        uint16
	espKey          []byte
	espIntegKey     []byte
	innerIP         net.IP // inner IP assigned by the ePDG (CP payload)
	innerIPv6       net.IP
	innerPrefix     int
	innerIPv6Prefix int
	dnsServers      []net.IP
	pcscfServers    []net.IP
	remoteIP        net.IP // ePDG outer address
	remotePort      int

	// --- lifecycle ---
	ctx      context.Context
	cancel   context.CancelFunc
	done     chan struct{}
	doneOnce sync.Once

	mu               sync.RWMutex
	childSAMu        sync.RWMutex
	ikeExchangeMu    sync.Mutex
	controlMu        sync.RWMutex
	controlWG        sync.WaitGroup
	controlResponses chan *ikev2.IKEPacket
	controlRunning   bool
	terminalErr      error
	initErr          error
	state            string
	dataPlaneStarted bool
	dataPlaneWG      sync.WaitGroup
	startedAt        time.Time
	lastPingAt       time.Time
	lastDPDAt        time.Time
	rekeyResetCh     chan struct{}
	lastIKERequest   []byte
	lastIKEResponse  []byte
	nextOutboundID   uint32

	// --- timers ---
	timersMu        sync.Mutex
	ikeReauthTimer  *time.Timer
	ikeRekeyTimer   *time.Timer
	childRekeyTimer *time.Timer
	natKeepalive    *time.Timer
	dpdTimer        *time.Timer

	// --- wireshark ---
	debug *WiresharkDebugger

	// --- IKE_SA_INIT proposal negotiation ---
	ikeProfileOffset         int
	offeredIKEProfiles       []string
	offeredIKEProposals      []*ikev2.Proposal
	effectiveCipherPolicy    string
	negotiationFallbackCount int
	sendCookie               bool
	fragmentationSupported   bool
}

// NewSession builds a SWu session from the configuration.
func NewSession(cfg *Config) *Session {
	if cfg == nil {
		cfg = &Config{}
	}
	ctx, cancel := context.WithCancel(context.Background())
	s := &Session{
		cfg:               cfg,
		ctx:               ctx,
		cancel:            cancel,
		done:              make(chan struct{}),
		state:             stateIdle,
		startedAt:         time.Now(),
		rekeyResetCh:      make(chan struct{}, 1),
		nonceLen:          cfg.NonceLen,
		nextOutboundID:    1,
		localIKEInitiator: true,
		fastReauthCtx:     initFastReauthContext(cfg),
	}
	if s.nonceLen <= 0 {
		s.nonceLen = 32
	}
	s.initErr = initializeSessionAlgorithms(s, cfg)
	if cfg.Wireshark {
		s.debug = NewWiresharkDebugger()
	}
	return s
}

func initFastReauthContext(cfg *Config) *engineeap.FastReauthContext {
	context := engineeap.NewFastReauthContext()
	if cfg != nil && cfg.FastReauthID != "" && len(cfg.FastReauthMK) > 0 {
		context.SaveReauthData(cfg.FastReauthID, cfg.FastReauthMK, cfg.FastReauthKEncr, cfg.FastReauthKAut)
	}
	return context
}

// setState records a session state transition and fires the callback.
func (s *Session) setState(st string) {
	s.mu.Lock()
	s.state = st
	s.mu.Unlock()
	if s.cfg != nil && s.cfg.OnStateChange != nil {
		s.cfg.OnStateChange(st)
	}
}

// State returns the current session state string.
func (s *Session) State() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.state
}

// setTerminalError records the terminal error and transitions to error state.
func (s *Session) setTerminalError(err error) {
	if err == nil {
		return
	}
	s.mu.Lock()
	if s.terminalErr == nil {
		s.terminalErr = err
	}
	s.mu.Unlock()
	s.setState(stateError)
}

// TerminalError returns the error that ended an established session, if any.
func (s *Session) TerminalError() error {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.terminalErr
}

func (s *Session) signalDone() {
	s.doneOnce.Do(func() { close(s.done) })
}

// Connect establishes the SWu tunnel: IKE_SA_INIT → IKE_AUTH (EAP-AKA) →
// CREATE_CHILD_SA. It retries on redirect (RFC 5685) and returns once the data
// plane is up or the session fails terminally.
func (s *Session) Connect(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	s.setState(stateConnecting)
	if s.initErr != nil {
		err := fmt.Errorf("swu: initialize session: %w", s.initErr)
		s.setTerminalError(err)
		return err
	}
	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		err := s.connectOnce(ctx)
		if err == nil {
			return nil
		}
		var redir *RedirectError
		if errors.As(err, &redir) {
			target := redir.NewAddr
			if target == "" {
				target = redir.Target
			}
			if s.cfg != nil && s.cfg.OnRedirect != nil {
				s.cfg.OnRedirect(target)
			}
			s.cfg.EPDGAddr, s.cfg.EpDGAddr = target, target
			s.resetForRedirect()
			lastErr = err
			continue
		}
		s.setTerminalError(err)
		return err
	}
	if lastErr != nil {
		s.setTerminalError(lastErr)
		return lastErr
	}
	return errors.New("swu: connect failed")
}

func (s *Session) resetForRedirect() {
	s.SPIi, s.SPIr = [8]byte{}, [8]byte{}
	s.Ni, s.nr, s.cookie = nil, nil, nil
	s.dh, s.ikeKeys, s.dhSharedSecret = nil, nil, nil
	s.sendCookie = false
	s.ikeProfileOffset = 0
}

// connectOnce runs a single IKE_SA_INIT → IKE_AUTH → CREATE_CHILD_SA attempt.
func (s *Session) connectOnce(ctx context.Context) (err error) {
	if s.cfg == nil {
		return errors.New("swu: no configuration")
	}
	if configuredEPDGAddress(s.cfg) == "" {
		return errors.New("swu: no ePDG address configured")
	}
	defer func() {
		if err != nil {
			s.stopDataPlane()
			s.stopTransport()
		}
	}()

	// Resolve the ePDG and build the IKE/ESP transport.
	if err := s.buildTransport(); err != nil {
		return fmt.Errorf("build transport: %w", err)
	}
	// IKE_SA_INIT.
	if err := s.runIKESAInit(ctx); err != nil {
		return err
	}

	// IKE_AUTH (EAP-AKA).
	if err := s.runIKEAuthLoop(ctx); err != nil {
		return err
	}

	// Some ePDGs establish the first CHILD_SA in IKE_AUTH. If the responder did
	// not return SAr2, create it explicitly with a CREATE_CHILD_SA exchange.
	if s.espRemoteSPI == 0 {
		if err := s.dispatchCreateChildSA(ctx); err != nil {
			return err
		}
	}

	// Bring up the data plane.
	if err := s.setupDataPlane(); err != nil {
		return fmt.Errorf("setup data plane: %w", err)
	}
	if err := s.startEstablishedDataPlane(); err != nil {
		return fmt.Errorf("start data plane: %w", err)
	}
	if err := s.ensureIKEDispatcher(); err != nil {
		return fmt.Errorf("start IKE control plane: %w", err)
	}
	s.setState(stateEstablished)
	s.startTimers()
	return nil
}

func (s *Session) stopTransport() {
	if s.socket == nil {
		return
	}
	s.socket.Stop()
	s.socket = nil
}

// runIKESAInit performs the IKE_SA_INIT exchange with COOKIE / INVALID_KE /
// REDIRECT handling.
func (s *Session) runIKESAInit(ctx context.Context) error {
	for {
		raw, err := s.buildIKESAInitPacket()
		if err != nil {
			return err
		}
		if err := s.sendIKE(raw); err != nil {
			return fmt.Errorf("send IKE_SA_INIT: %w", err)
		}
		resp, err := s.receiveIKE(ctx)
		if err != nil {
			return fmt.Errorf("receive IKE_SA_INIT response: %w", err)
		}
		responseData, err := resp.Encode()
		if err != nil {
			return fmt.Errorf("encode received IKE_SA_INIT response: %w", err)
		}
		err = s.handleIKESAInitResp(responseData)
		if err == nil {
			s.mu.Lock()
			s.ikeSAInitRequest = append([]byte(nil), s.lastIKERequest...)
			s.ikeSAInitResponse = append([]byte(nil), s.lastIKEResponse...)
			s.mu.Unlock()
			return nil
		}
		if errors.Is(err, errCookieRequired) {
			continue // resend with the cookie
		}
		var negotiationError *NegotiationError
		if errors.As(err, &negotiationError) && negotiationError.Retryable && s.advanceIKEProfileOffset() {
			s.resetIKEInitMaterial()
			continue
		}
		var groupError *ErrInvalidKEGroup
		if errors.As(err, &groupError) {
			if err := s.selectRequestedDHGroup(groupError); err != nil {
				return err
			}
			continue
		}
		return err
	}
}

// buildTransport resolves the ePDG and opens the IKE/ESP socket.
func (s *Session) buildTransport() error {
	endpoint := configuredEPDGAddress(s.cfg)
	host, port := endpoint, "500"
	if h, p, err := net.SplitHostPort(endpoint); err == nil {
		host, port = h, p
	} else if s.cfg.EpDGPort != 0 {
		port = fmt.Sprintf("%d", s.cfg.EpDGPort)
	}
	if strings.TrimSpace(s.cfg.ProxyAddr) != "" {
		return s.buildProxyTransport(host, port)
	}
	localIP := configuredLocalIP(s.cfg)
	if localIP == nil {
		ip, err := detectOutboundIPv4ByHost(host, port)
		if err != nil {
			return fmt.Errorf("detect outbound IP: %w", err)
		}
		localIP = ip
	}
	localAddr := net.JoinHostPort(localIP.String(), "0")
	targetAddr := net.JoinHostPort(host, port)
	sm, err := ipsec.NewSocketManager(s.cfg.DeviceID, localAddr, targetAddr, s.cfg.DNSServer)
	if err != nil {
		return fmt.Errorf("open IKE socket: %w", err)
	}
	if err := sm.Start(); err != nil {
		sm.Stop()
		return fmt.Errorf("start IKE socket: %w", err)
	}
	s.socket = sm
	s.remoteIP = sm.RemoteIP()
	s.remotePort = sm.RemotePort()
	return nil
}

func (s *Session) buildProxyTransport(host, port string) error {
	proxyCfg := ipsec.Socks5Config{}
	if s.cfg.Proxy != nil {
		proxyCfg = *s.cfg.Proxy
	}
	targetAddr := net.JoinHostPort(host, port)
	proxyCfg.ProxyAddr = s.cfg.ProxyAddr
	proxyCfg.RemoteAddr = targetAddr
	proxyCfg.DNSServer = s.cfg.DNSServer
	proxyCfg.DeviceID = s.cfg.DeviceID
	transport, err := ipsec.NewSocks5Transport(proxyCfg)
	if err != nil {
		return fmt.Errorf("open SOCKS5 IKE transport: %w", err)
	}
	if err := transport.Start(); err != nil {
		transport.Stop()
		return fmt.Errorf("start SOCKS5 IKE transport: %w", err)
	}
	s.socket = transport
	s.remoteIP = transport.RemoteIP()
	s.remotePort = transport.RemotePort()
	return nil
}

func configuredEPDGAddress(cfg *Config) string {
	if cfg.EPDGAddr != "" {
		return cfg.EPDGAddr
	}
	return cfg.EpDGAddr
}

func configuredLocalIP(cfg *Config) net.IP {
	if cfg.LocalIP != nil {
		return cfg.LocalIP
	}
	return net.ParseIP(cfg.LocalAddr)
}

// detectOutboundIPv4ByHost resolves a configured hostname before invoking the
// original IP-based outbound-route detector.
func detectOutboundIPv4ByHost(host, port string) (net.IP, error) {
	remoteIP, err := detectOutboundRoute(host)
	if err != nil {
		return nil, err
	}
	remotePort, err := net.LookupPort("udp", port)
	if err != nil {
		return nil, err
	}
	return detectOutboundIPv4(remoteIP, uint16(remotePort))
}

// detectOutboundRoute resolves the ePDG host to an IP (used by buildTransport).
func detectOutboundRoute(host string) (net.IP, error) {
	ips, err := net.LookupIP(host)
	if err != nil {
		return nil, err
	}
	for _, ip := range ips {
		if ip.To4() != nil {
			return ip, nil
		}
	}
	if len(ips) > 0 {
		return ips[0], nil
	}
	return nil, errors.New("swu: no address for ePDG")
}

// Shutdown tears down the session: stops timers, closes the transport and the
// data plane, and marks the session done.
func (s *Session) Shutdown() {
	s.mu.Lock()
	if s.state == stateShutdown {
		s.mu.Unlock()
		return
	}
	s.state = stateShutdown
	s.mu.Unlock()

	s.cancel()
	s.stopTimers()
	cleanupErr := s.stopDataPlane()
	s.controlWG.Wait()
	s.dataPlaneWG.Wait()
	s.stopTransport()
	if cleanupErr != nil {
		s.mu.Lock()
		if s.terminalErr == nil {
			s.terminalErr = fmt.Errorf("swu: clean up data plane: %w", cleanupErr)
		}
		s.mu.Unlock()
	}
	s.signalDone()
}

// WaitDone blocks until the session is shut down.
func (s *Session) WaitDone() {
	<-s.done
}

// WaitDoneContext blocks until the session is shut down or the context ends.
func (s *Session) WaitDoneContext(ctx context.Context) error {
	select {
	case <-s.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Reauthenticate forces a full re-authentication (new IKE_SA_INIT).
func (s *Session) Reauthenticate() error {
	s.mu.RLock()
	established := s.state == stateEstablished
	s.mu.RUnlock()
	if !established {
		return errors.New("swu: session not established")
	}
	return ErrFreshRuntimeRequired
}

// Snapshot returns a summary of the session state.
func (s *Session) Snapshot() map[string]interface{} {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return map[string]interface{}{
		"state":      s.state,
		"epdg":       s.cfg.EPDGAddr,
		"remote_ip":  s.remoteIP.String(),
		"remote_pt":  s.remotePort,
		"inner_ip":   s.primaryInnerIP().String(),
		"started_at": s.startedAt,
	}
}

// InnerNetworkConfig is the address configuration assigned by the ePDG.
type InnerNetworkConfig struct {
	IPv4          net.IP
	IPv6          net.IP
	PrefixLen     int
	IPv6PrefixLen int
	DNS           []net.IP
	PCSCF         []net.IP
}

// InnerNetwork returns a copy of the negotiated inner network configuration.
func (s *Session) InnerNetwork() InnerNetworkConfig {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return InnerNetworkConfig{
		IPv4: append(net.IP(nil), s.innerIP...), IPv6: append(net.IP(nil), s.innerIPv6...),
		PrefixLen: s.innerPrefix, IPv6PrefixLen: s.innerIPv6Prefix,
		DNS: cloneIPs(s.dnsServers), PCSCF: cloneIPs(s.pcscfServers),
	}
}

func (s *Session) primaryInnerIP() net.IP {
	if s.innerIP != nil {
		return s.innerIP
	}
	return s.innerIPv6
}

// InnerPacketIO returns the packet boundary for the user-space IMS stack.
func (s *Session) InnerPacketIO() InnerPacketIO {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.innerEndpoint
}

func cloneIPs(in []net.IP) []net.IP {
	out := make([]net.IP, 0, len(in))
	for _, ip := range in {
		out = append(out, append(net.IP(nil), ip...))
	}
	return out
}

// NextSequenceNumber returns the next ESP sequence number for the outbound SA.
func (s *Session) NextSequenceNumber() uint32 {
	s.childSAMu.RLock()
	defer s.childSAMu.RUnlock()
	if s.espOutboundSA == nil {
		return 0
	}
	return s.espOutboundSA.NextSequenceNumber()
}

// InnerPacketEndpoint returns the user-space inner packet endpoint.
func (s *Session) InnerPacketEndpoint() *userspaceInnerPacketEndpoint {
	return s.innerEndpoint
}

// startTimers arms the rekey / reauth / keepalive / DPD timers.
func (s *Session) startTimers() {
	s.startIKEReauthTimer()
	s.startIKESARekeyTimer()
	s.startChildSARekeyTimer()
	s.startNATKeepalive()
	s.startDPD()
}

// stopTimers stops all timers.
func (s *Session) stopTimers() {
	s.timersMu.Lock()
	defer s.timersMu.Unlock()
	timers := []*time.Timer{s.ikeReauthTimer, s.ikeRekeyTimer, s.childRekeyTimer, s.natKeepalive, s.dpdTimer}
	for _, t := range timers {
		if t != nil {
			t.Stop()
		}
	}
	s.ikeReauthTimer = nil
	s.ikeRekeyTimer = nil
	s.childRekeyTimer = nil
	s.natKeepalive = nil
	s.dpdTimer = nil
}

func (s *Session) armTimer(target **time.Timer, delay time.Duration, callback func()) {
	s.timersMu.Lock()
	defer s.timersMu.Unlock()
	if s.ctx.Err() != nil {
		return
	}
	*target = time.AfterFunc(delay, callback)
}

// startIKEReauthTimer arms the periodic re-authentication timer.
func (s *Session) startIKEReauthTimer() {
	every := s.cfg.ReauthSeconds
	if every <= 0 {
		every = 24 * time.Hour
	}
	s.armTimer(&s.ikeReauthTimer, every, func() {
		if err := s.Reauthenticate(); err != nil {
			s.failEstablishedControl(fmt.Errorf("swu: IKE reauthentication failed: %w", err))
			return
		}
		s.startIKEReauthTimer()
	})
}

// startIKESARekeyTimer arms the IKE SA rekey timer.
func (s *Session) startIKESARekeyTimer() {
	every := s.cfg.RekeyIKESeconds
	if every <= 0 {
		every = 8 * time.Hour
	}
	s.armTimer(&s.ikeRekeyTimer, every, func() {
		if err := s.RekeyIKESA(); err != nil {
			s.failEstablishedControl(fmt.Errorf("swu: IKE SA rekey failed: %w", err))
			return
		}
		s.startIKESARekeyTimer()
	})
}

// startChildSARekeyTimer arms the CHILD_SA rekey timer.
func (s *Session) startChildSARekeyTimer() {
	every := s.cfg.RekeyChildSeconds
	if every <= 0 {
		every = 1 * time.Hour
	}
	s.armTimer(&s.childRekeyTimer, every, func() {
		if err := s.RekeyChildSA(); err != nil {
			s.failEstablishedControl(fmt.Errorf("swu: CHILD_SA rekey failed: %w", err))
			return
		}
		s.startChildSARekeyTimer()
	})
}

// startNATKeepalive arms the NAT keepalive timer (RFC 3948 §2.4).
func (s *Session) startNATKeepalive() {
	every := s.cfg.NATKeepaliveEvery
	if every <= 0 {
		every = 20 * time.Second
	}
	s.armTimer(&s.natKeepalive, every, func() {
		if err := s.sendNATKeepalive(); err != nil {
			s.failEstablishedControl(fmt.Errorf("swu: NAT keepalive failed: %w", err))
			return
		}
		s.startNATKeepalive()
	})
}

// sendNATKeepalive sends a NAT keepalive packet on the ESP transport.
func (s *Session) sendNATKeepalive() error {
	if s.socket == nil {
		return errors.New("swu: no IKE transport")
	}
	s.mu.Lock()
	s.lastPingAt = time.Now()
	s.mu.Unlock()
	return s.socket.SendNATKeepalive()
}

// startDPD arms the dead-peer-detection timer (RFC 7296 §1.4.2).
func (s *Session) startDPD() {
	every := s.cfg.DPDProbeEvery
	if every <= 0 {
		every = 30 * time.Second
	}
	s.armTimer(&s.dpdTimer, every, func() {
		if err := s.DPDProbe(); err != nil {
			s.failEstablishedControl(fmt.Errorf("swu: DPD failed: %w", err))
			return
		}
		s.startDPD()
	})
}

// DPDProbe sends an INFORMATIONAL request to verify the peer is alive.
func (s *Session) DPDProbe() error {
	s.ikeExchangeMu.Lock()
	defer s.ikeExchangeMu.Unlock()
	if s.socket == nil {
		return errors.New("swu: no transport")
	}
	s.mu.Lock()
	s.lastDPDAt = time.Now()
	s.mu.Unlock()
	pkt := &ikev2.IKEPacket{
		Header: newIKEHeader(
			s.SPIi, s.SPIr, ikev2.INFORMATIONAL, s.localIKEFlags(false), s.nextMessageID(),
		),
	}
	payloads, err := s.exchangeEstablishedIKE(s.ctx, pkt)
	if err != nil {
		return err
	}
	if len(payloads) != 0 {
		return fmt.Errorf("swu: DPD response contains unexpected payloads %s", ikePayloadTypes(payloads))
	}
	return nil
}

// nextMessageID returns the next initiator request ID. IKE_SA_INIT uses zero;
// subsequent exchanges start at one and increase monotonically (RFC 7296).
func (s *Session) nextMessageID() uint32 {
	s.mu.Lock()
	defer s.mu.Unlock()
	id := s.nextOutboundID
	s.nextOutboundID++
	return id
}

// logSessionStats logs data-plane counters (no-op without a debugger).
func (s *Session) logSessionStats() {
	if s.debug == nil {
		return
	}
	s.debug.LogRaw("session stats")
}

// logDataPlaneStats logs inner-packet counters.
func (s *Session) logDataPlaneStats() {
	if s.debug == nil || s.innerEndpoint == nil {
		return
	}
	s.debug.LogRaw(s.innerEndpoint.Snapshot())
}

// StartDPD starts the dead-peer-detection timer (RFC 7296 §1.4.2).
func (s *Session) StartDPD() {
	if s == nil {
		return
	}
	s.startDPD()
}
