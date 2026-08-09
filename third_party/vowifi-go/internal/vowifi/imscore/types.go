// Package imscore is the IMS core: SIP registration (Digest-AKA), dialog
// management, and SMS/USSD-over-IMS.
//
// Reconstructed from the decompiled internal/vowifi/imscore (RFC 3261, RFC
// 2617, RFC 3310, 3GPP TS 24.229, TS 24.390).
package imscore

import (
	"context"
	"errors"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	enginesim "github.com/iniwex5/vowifi-go/engine/sim"
	"github.com/iniwex5/vowifi-go/internal/smscodec"
	"github.com/iniwex5/vowifi-go/internal/vowifi/common"
	"github.com/iniwex5/vowifi-go/internal/vowifi/ussi"
)

// IMS registration states (recovered from the decompiled registration_state.go).
const (
	regIdle        = "idle"
	regRegistering = "registering"
	regRegistered  = "registered"
	regReregister  = "reregistering"
	regFailed      = "failed"
	regUnregister  = "unregistering"
)

// IMSRegisterTemplate is the carrier-specific REGISTER wire policy.
type IMSRegisterTemplate struct {
	Expires                   time.Duration
	Transport                 string
	SupportedHeader           string
	AllowHeader               string
	ContactMode               string
	AccessType                string
	ICSIRef                   string
	ContactOrder              []string
	IncludePANIAuthenticated  bool
	StrictSecurityServerOffer bool
}

// AKAProvider computes AKA from the network challenge.
type AKAProvider = enginesim.AKAProvider

// AKAResult is the outcome of an AKA computation.
type AKAResult = enginesim.AKAResult

// DialOptions controls a connection created on an IMS network.
//
// The fields mirror the original network boundary. Timeout and KeepAlive are
// durations represented as nanoseconds; TCPMSS overrides the endpoint MSS when
// it is positive.
type DialOptions struct {
	Timeout   int64
	KeepAlive int64
	TCPMSS    int
}

// IMSNetwork is the network surface used by the IMS stack.
type IMSNetwork interface {
	LocalIP() net.IP
	HasLocalIP(ip net.IP) bool
	ResolveIP(ctx context.Context, host string) (net.IP, error)
	DialContext(ctx context.Context, network, addr string) (net.Conn, error)
	DialTCPContext(ctx context.Context, local, remote *net.TCPAddr) (net.Conn, error)
	ListenTCP(addr *net.TCPAddr) (net.Listener, error)
	ListenPacket(network string, addr *net.UDPAddr) (net.PacketConn, error)
}

// SystemIMSNetwork is the default IMS network implementation.
type SystemIMSNetwork struct {
	localIP net.IP
}

// NewSystemIMSNetwork creates a network with the given local IP.
func NewSystemIMSNetwork(localIP net.IP) *SystemIMSNetwork {
	return &SystemIMSNetwork{localIP: localIP}
}

// LocalIP returns the local IP.
func (n *SystemIMSNetwork) LocalIP() net.IP { return n.localIP }

// HasLocalIP reports whether the network has the address.
func (n *SystemIMSNetwork) HasLocalIP(ip net.IP) bool {
	if ip == nil {
		return false
	}
	if n.localIP != nil && n.localIP.Equal(ip) {
		return true
	}
	return common.HostHasIP(ip.String())
}

// ResolveIP resolves a host to an IP.
func (n *SystemIMSNetwork) ResolveIP(ctx context.Context, host string) (net.IP, error) {
	ips, err := n.LookupIPs(ctx, host)
	if err != nil {
		return nil, err
	}
	if len(ips) > 0 {
		return ips[0], nil
	}
	return nil, net.ErrClosed
}

// LookupIPs returns every resolved address so security policy can retain the
// local address family when DNS returns mixed A and AAAA records.
func (n *SystemIMSNetwork) LookupIPs(ctx context.Context, host string) ([]net.IP, error) {
	return net.DefaultResolver.LookupIP(ctx, "ip", host)
}

// LookupSRV resolves a SIP service endpoint.
func (n *SystemIMSNetwork) LookupSRV(ctx context.Context, service, proto, name string) (string, uint16, error) {
	_, records, err := net.DefaultResolver.LookupSRV(ctx, service, proto, name)
	if err != nil {
		return "", 0, err
	}
	if len(records) == 0 {
		return "", 0, errors.New("imscore: no SRV records")
	}
	return strings.TrimSuffix(records[0].Target, "."), records[0].Port, nil
}

// DialContext dials a TCP connection.
func (n *SystemIMSNetwork) DialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	var d net.Dialer
	return d.DialContext(ctx, network, addr)
}

// DialTCPContext dials TCP from an explicit local IMS address and port.
func (n *SystemIMSNetwork) DialTCPContext(ctx context.Context, local, remote *net.TCPAddr) (net.Conn, error) {
	dialer := net.Dialer{LocalAddr: local}
	return dialer.DialContext(ctx, "tcp", remote.String())
}

// ListenTCP listens for TCP connections.
func (n *SystemIMSNetwork) ListenTCP(addr *net.TCPAddr) (net.Listener, error) {
	return net.ListenTCP("tcp", addr)
}

// ListenPacket listens for UDP packets.
func (n *SystemIMSNetwork) ListenPacket(network string, addr *net.UDPAddr) (net.PacketConn, error) {
	return net.ListenUDP("udp", addr)
}

// Service is the IMS core service.
type Service struct {
	cfg *IMSConfig
	registrationRuntime

	mu          sync.RWMutex
	registerMu  sync.Mutex
	subscribeMu sync.Mutex
	state       string
	regState    string

	// Registration state.
	regSession *registerSession
	spiPairs   [][2]uint32

	// SIP transport.
	transport                *sipTransport
	registrationIO           net.PacketConn
	registrationTCP          net.Conn
	registrationPreviousTCP  net.Conn
	registrationTCPProtected bool
	registrationTransport    string
	securityServerIO         net.Listener
	clientPortReserve        net.Listener
	registrationRemote       *net.UDPAddr
	protectedClientPort      int
	protectedServerPort      int
	externalTransport        bool
	protectedConnMu          sync.Mutex
	protectedConns           map[net.Conn]struct{}
	sipWriteMu               sync.Mutex
	receiverMu               sync.Mutex
	activeReceivers          int
	networkDone              sync.WaitGroup
	registerErrors           chan error
	keepaliveOnce            sync.Once
	keepaliveSuccessOnce     sync.Once
	maintenanceWake          chan struct{}
	registrationRefreshAt    time.Time
	keepaliveInterval        time.Duration
	keepaliveTimeout         time.Duration
	keepaliveFailureLimit    int
	keepaliveFailures        int

	// Dialogs.
	dialogRegistry *dialogRegistry
	endpointCSeq   atomic.Uint32
	serverTxMu     sync.Mutex
	serverTx       map[string]trackedServerTransaction
	serverTimers   serverTransactionTimers

	inboundSeenMu  sync.Mutex
	inboundSeen    map[string]time.Time
	inboundSeenRsp map[string]inboundRequestResponseMemo

	// Event bus.
	bus *imsEventBus

	// USSD.
	ussd *ussi.Service

	// Voice request routing.
	voiceHandler VoiceRequestHandler

	// Delivery store.
	delivery DeliveryStore

	// Callbacks and SMS capability state.
	onRegistered          func()
	onSMSReadiness        func(SMSReadiness)
	smsReceiverReady      bool
	nextSMSRPMR           byte
	nextSMSConcatRef      byte
	nextSIPCSeq           int
	smsReassembler        *smscodec.Reassembler
	smsTransactionTimeout time.Duration
	smsReportTimeout      time.Duration

	lastPingAt             time.Time
	securityVerify         string
	effectiveSecurityMode  string
	securityFallbackReason string
	securityFallbackCount  atomic.Int64
	signalingGeneration    uint64
	signalingReady         bool
	signalingFailureReason string
	lastError              string
	serviceRoute           string
	path                   string
	assocMSISDN            string
	learnedAOR             string

	lastRegisterTraceID   string
	lastRegisterAttemptAt time.Time
	lastRegisterOKAt      time.Time
	lastRegisterErr       string

	stop chan struct{}
}

type registrationRuntime struct {
	callID              string
	fromTag             string
	cseq                atomic.Uint32
	expires             uint32
	authRealm           string
	challengeRealm      string
	registrar           string
	registrarCandidates []string
	registrarIndex      int
	registrarSource     string
	regStatus           atomic.Int32
	nextRegister        time.Time
	lastSIPCode         atomic.Int32
	lastSIPText         string
	reRegisterPending   atomic.Bool
	regFailCount        atomic.Int32
	OnReconnectNeeded   func()
	reconnectTriggering atomic.Bool
}

// SMSReadiness describes the independently verifiable IMS SMS prerequisites.
type SMSReadiness struct {
	Registered     bool
	ProfileReady   bool
	TransportReady bool
	ReceiverReady  bool
	SMSCPresent    bool
	Ready          bool
	Reason         string
}

// ServiceStatus is a snapshot of the IMS service state.
type ServiceStatus struct {
	Enabled                bool
	DeviceID               string
	Registered             bool
	RegStatus              string
	Registrar              string
	RegistrarCandidates    []string
	RegistrarIndex         int
	RegistrarSource        string
	LastSIPCode            int
	LastSIPText            string
	Domain                 string
	IMPI                   string
	IMPU                   string
	Transport              string
	SMSReceiverTransport   string
	LocalAddr              string
	LocalPort              int
	IPSecInstalled         bool
	RXRunning              bool
	RXPort                 int
	TCPSignalingRunning    bool
	TCPSignalingConnected  bool
	EffectiveSecurityMode  string
	SecurityFallbackReason string
	SecurityFallbackCount  int64
	SignalingGeneration    uint64
	SignalingReady         bool
	SignalingFailureReason string
	RegFailCount           int
	ReRegisterPending      bool
	PingFailCount          int
	LastPingAt             time.Time
	LastPingOK             bool
	LastRegisterTraceID    string
	LastRegisterAttemptAt  time.Time
	LastRegisterOKAt       time.Time
	LastRegisterErr        string
	LastSMSSendTraceID     string
	LastSMSSendAt          time.Time
	LastSMSSendErr         string
	ServiceRoute           string
	Path                   string
	SecurityVerify         string
	AssociatedMSISDN       string
	LastError              string
	FragmentAudit          map[string]interface{}
	IMSEventBus            map[string]interface{}
	Diagnostics            map[string]interface{}

	// Compatibility fields added after v1.5.5.
	State    string
	RegState string
	IMPUs    []string
}

// IsRegistered reports whether the service is registered.
func (s ServiceStatus) IsRegistered() bool {
	return s.Registered || strings.EqualFold(strings.TrimSpace(s.RegStatus), "Registered")
}

// DeliveryStore persists SMS delivery state.
type DeliveryStore interface {
	CreateSMSDelivery(messageID, imsi, deviceID, peer, content string, partsTotal int, at time.Time) error
	UpsertSMSDeliveryPart(messageID string, partNo int, callID string, rpMR int, state string, sentAt time.Time) error
	MarkSMSDeliveryPartReport(inReplyTo, callID, deviceID string, rpMR int, state string, sipCode int, rpCause int, errText string, at time.Time) (DeliveryPartMatch, error)
	RecomputeSMSDelivery(messageID string, at time.Time) error
	UpdateSMSDeliveryState(messageID, state, lastError string, acks int, at time.Time) error
	GetSMSDeliveryStatus(messageID string) (*DeliveryStatus, error)
}

// SMSDeliverySIPResultStore persists the final response to the outbound
// MESSAGE transaction separately from the later RP delivery report.
type SMSDeliverySIPResultStore interface {
	MarkSMSDeliveryPartSIPResult(messageID string, partNo, sipCode int, state, errText string, at time.Time) error
}

// DeliveryPartMatch identifies a delivery part.
type DeliveryPartMatch struct {
	MessageID string
	PartNo    int
	State     string
	Matched   bool
}

// DeliveryStatus is the SMS delivery status.
type DeliveryStatus struct {
	MessageID  string
	IMSI       string
	DeviceID   string
	Peer       string
	Content    string
	PartsTotal int
	Acks       int
	State      string
	LastError  string
	Parts      []DeliveryPartStatus
}

// DeliveryPartStatus is one delivery part.
type DeliveryPartStatus struct {
	PartNo  int
	CallID  string
	State   string
	SIPCode int
	RPCause int
}
