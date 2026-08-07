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
	"github.com/iniwex5/vowifi-go/engine/ikev2"
	"github.com/iniwex5/vowifi-go/engine/ipsec"
	enginesim "github.com/iniwex5/vowifi-go/engine/sim"
	"github.com/iniwex5/vowifi-go/engine/swu/eapaka"
)

// Config carries the SWu session configuration recovered from the decompiled
// engine/swu. It is the input to NewSession.
type Config struct {
	// EPDGAddr is the ePDG host (FQDN or IP) and optional port.
	EPDGAddr string
	// APN is carried as IDr in the first IKE_AUTH request (3GPP TS 24.302).
	// Empty requests the operator's default APN.
	APN string
	// LocalIP is the local address to bind the IKE/ESP socket to.
	LocalIP net.IP
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
	// AlgorithmPolicy selects the IKE/ESP algorithm offer policy.
	AlgorithmPolicy string
	// IKEEncryption / IKEPRF / IKEIntegrity / IKEDH are the IKE algorithm
	// transform IDs (RFC 7296 §3.3.2). Zero selects the policy default.
	IKEEncryption uint16
	IKEPRF        uint16
	IKEIntegrity  uint16
	IKEDH         uint16
	// ESPEncryption / ESPIntegrity are the ESP transform IDs for the CHILD_SA.
	ESPEncryption uint16
	ESPIntegrity  uint16
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

// DataplaneModeUserspace selects the user-space data plane (recovered from
// the decompiled dataplane selection).
const DataplaneModeUserspace = "userspace"

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
	aead           bool
	espEncKeyLen   int
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
	innerEndpoint *userspaceInnerPacketEndpoint
	espOutboundSA *ipsec.SecurityAssociation
	espInboundSA  *ipsec.SecurityAssociation
	espLocalSPI   uint32
	espRemoteSPI  uint32
	childNi       []byte
	childNr       []byte
	childTSi      *ikev2.EncryptedPayloadTS
	childTSr      *ikev2.EncryptedPayloadTS
	espCipher     uint16
	espInteg      uint16
	espKey        []byte
	espIntegKey   []byte
	innerIP       net.IP // inner IP assigned by the ePDG (CP payload)
	innerIPv6     net.IP
	innerPrefix   int
	dnsServers    []net.IP
	remoteIP      net.IP // ePDG outer address
	remotePort    uint16

	// --- lifecycle ---
	ctx    context.Context
	cancel context.CancelFunc
	done   chan struct{}

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
	ikeReauthTimer  *time.Timer
	ikeRekeyTimer   *time.Timer
	childRekeyTimer *time.Timer
	natKeepalive    *time.Timer
	dpdTimer        *time.Timer

	// --- wireshark ---
	debug *WiresharkDebugger
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

// terminalError returns the recorded terminal error, if any.
func (s *Session) terminalError() error {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.terminalErr
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
			if s.cfg != nil && s.cfg.OnRedirect != nil {
				s.cfg.OnRedirect(redir.Target)
			}
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

// connectOnce runs a single IKE_SA_INIT → IKE_AUTH → CREATE_CHILD_SA attempt.
func (s *Session) connectOnce(ctx context.Context) (err error) {
	if s.cfg == nil {
		return errors.New("swu: no configuration")
	}
	if s.cfg.EPDGAddr == "" {
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
	for attempt := 0; attempt < 3; attempt++ {
		pkt, err := s.buildIKESAInitPacket()
		if err != nil {
			return err
		}
		raw := pkt.Encode()
		if err := s.sendIKE(raw); err != nil {
			return fmt.Errorf("send IKE_SA_INIT: %w", err)
		}
		resp, err := s.receiveIKE(ctx)
		if err != nil {
			return fmt.Errorf("receive IKE_SA_INIT response: %w", err)
		}
		err = s.handleIKESAInitResp(resp)
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
		return err
	}
	return errors.New("swu: IKE_SA_INIT failed after retries")
}

// buildTransport resolves the ePDG and opens the IKE/ESP socket.
func (s *Session) buildTransport() error {
	host, port := s.cfg.EPDGAddr, "500"
	if h, p, err := net.SplitHostPort(s.cfg.EPDGAddr); err == nil {
		host, port = h, p
	}
	if strings.TrimSpace(s.cfg.ProxyAddr) != "" {
		return s.buildProxyTransport(host, port)
	}
	localIP := s.cfg.LocalIP
	if localIP == nil {
		ip, err := detectOutboundIPv4(host, port)
		if err != nil {
			return fmt.Errorf("detect outbound IP: %w", err)
		}
		localIP = ip
	}
	localAddr := net.JoinHostPort(localIP.String(), "0")
	sm, err := ipsec.NewSocketManager(localIP.String(), localAddr, host, port)
	if err != nil {
		return fmt.Errorf("open IKE socket: %w", err)
	}
	s.socket = sm
	s.remoteIP = sm.RemoteIP()
	s.remotePort = sm.RemotePort()
	sm.Start()
	return nil
}

func (s *Session) buildProxyTransport(host, port string) error {
	proxyCfg := ipsec.Socks5Config{}
	if s.cfg.Proxy != nil {
		proxyCfg = *s.cfg.Proxy
	}
	targetAddr := net.JoinHostPort(host, port)
	transport, err := ipsec.NewSocks5Transport(proxyCfg, s.cfg.ProxyAddr, targetAddr, 10*time.Second)
	if err != nil {
		return fmt.Errorf("open SOCKS5 IKE transport: %w", err)
	}
	s.socket = transport
	s.remoteIP = transport.RemoteIP()
	s.remotePort = transport.RemotePort()
	transport.Start()
	return nil
}

// detectOutboundIPv4 finds the local IPv4 address used to reach the ePDG.
func detectOutboundIPv4(host, port string) (net.IP, error) {
	remoteIP, err := detectOutboundRoute(host)
	if err != nil {
		return nil, err
	}
	remote := net.JoinHostPort(remoteIP.String(), port)
	conn, err := net.Dial("udp", remote)
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	addr, ok := conn.LocalAddr().(*net.UDPAddr)
	if !ok {
		return nil, errors.New("swu: no UDP local address")
	}
	return addr.IP, nil
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

	s.stopTimers()
	s.cancel()
	s.controlWG.Wait()
	s.dataPlaneWG.Wait()
	s.stopDataPlane()
	s.stopTransport()
	select {
	case <-s.done:
	default:
		close(s.done)
	}
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
	return errors.New("swu: full reauthentication requires a fresh runtime session")
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
		"inner_ip":   s.innerIP.String(),
		"started_at": s.startedAt,
	}
}

// InnerNetworkConfig is the address configuration assigned by the ePDG.
type InnerNetworkConfig struct {
	IPv4      net.IP
	IPv6      net.IP
	PrefixLen int
	DNS       []net.IP
}

// InnerNetwork returns a copy of the negotiated inner network configuration.
func (s *Session) InnerNetwork() InnerNetworkConfig {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return InnerNetworkConfig{
		IPv4: append(net.IP(nil), s.innerIP...), IPv6: append(net.IP(nil), s.innerIPv6...),
		PrefixLen: s.innerPrefix, DNS: cloneIPs(s.dnsServers),
	}
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
	for _, t := range []*time.Timer{s.ikeReauthTimer, s.ikeRekeyTimer, s.childRekeyTimer, s.natKeepalive, s.dpdTimer} {
		if t != nil {
			t.Stop()
		}
	}
}

// startIKEReauthTimer arms the periodic re-authentication timer.
func (s *Session) startIKEReauthTimer() {
	every := s.cfg.ReauthSeconds
	if every <= 0 {
		every = 24 * time.Hour
	}
	s.ikeReauthTimer = time.AfterFunc(every, func() {
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
	s.ikeRekeyTimer = time.AfterFunc(every, func() {
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
	s.childRekeyTimer = time.AfterFunc(every, func() {
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
	s.natKeepalive = time.AfterFunc(every, func() {
		s.sendNATKeepalive()
		s.startNATKeepalive()
	})
}

// sendNATKeepalive sends a NAT keepalive packet on the ESP transport.
func (s *Session) sendNATKeepalive() {
	if s.socket == nil {
		return
	}
	s.mu.Lock()
	s.lastPingAt = time.Now()
	s.mu.Unlock()
	s.socket.SendNATKeepalive()
}

// startDPD arms the dead-peer-detection timer (RFC 7296 §1.4.2).
func (s *Session) startDPD() {
	every := s.cfg.DPDProbeEvery
	if every <= 0 {
		every = 30 * time.Second
	}
	s.dpdTimer = time.AfterFunc(every, func() {
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
		InitiatorSPI: s.SPIi,
		ResponderSPI: s.SPIr,
		Version:      0x20,
		ExchangeType: ikev2.ExchangeInformational,
		Flags:        s.localIKEFlags(false),
		MessageID:    s.nextMessageID(),
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
