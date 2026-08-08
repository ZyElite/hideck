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
	"time"

	enginesim "github.com/iniwex5/vowifi-go/engine/sim"
	"github.com/iniwex5/vowifi-go/internal/smscodec"
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

// IMSConfig is the IMS configuration for a session.
type IMSConfig struct {
	// DeviceID identifies the device.
	DeviceID string
	// IMEI identifies the mobile equipment used for the IMS instance URN.
	IMEI string
	// IMSI is the subscriber IMSI.
	IMSI string
	// IMPI / IMPU are the IMS identities.
	IMPI string
	IMPU []string
	// Domain is the IMS domain.
	Domain string
	// SMSC is the service-centre address used for RP-DATA messages.
	SMSC string
	// Realm is the digest realm.
	Realm string
	// EPDGAddr is the ePDG address.
	EPDGAddr string
	// LocalIP is the local SIP address.
	LocalIP net.IP
	// Transport is the SIP transport ("auto"/"tcp"/"udp").
	Transport string
	// Registrar is the registrar host:port.
	Registrar string
	// LocalPort is the local SIP port selected on IMSNetwork.
	LocalPort int
	// Expires is the registration interval.
	Expires time.Duration
	// AKAProvider computes AKA (RAND, AUTN) -> (RES, CK, IK).
	AKAProvider AKAProvider
	// IMSNetwork is the network surface.
	IMSNetwork IMSNetwork
	// DeliveryStore persists SMS delivery state.
	DeliveryStore DeliveryStore
	// EventBus receives IMS events.
	EventBus *imsEventBus
	// IPSec3GPPEnabled enables the 3GPP IPsec security.
	IPSec3GPPEnabled bool
	// TraceID is the session trace ID.
	TraceID string
	// UserAgent is the SIP User-Agent header value.
	UserAgent string
	// CellularNetworkInfo is the 3GPP cellular network snapshot advertised by
	// the recovered client on REGISTER requests.
	CellularNetworkInfo string
	// PAccessNetworkCountry is appended to authenticated PANI headers.
	PAccessNetworkCountry string
	// RegisterTemplate carries the recovered carrier-specific REGISTER wire
	// policy selected by the runtime host.
	RegisterTemplate IMSRegisterTemplate
}

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
	return n.localIP != nil && n.localIP.Equal(ip)
}

// ResolveIP resolves a host to an IP.
func (n *SystemIMSNetwork) ResolveIP(ctx context.Context, host string) (net.IP, error) {
	ips, err := net.DefaultResolver.LookupIP(ctx, "ip", host)
	if err != nil {
		return nil, err
	}
	if len(ips) > 0 {
		return ips[0], nil
	}
	return nil, net.ErrClosed
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
	refreshTimer             *time.Timer
	registerErrors           chan error

	// Dialogs.
	dialogs *dialogRegistry

	// Event bus.
	bus *imsEventBus

	// USSD.
	ussd *ussi.Service

	// Voice request routing.
	voiceHandler VoiceRequestHandler

	// Delivery store.
	delivery DeliveryStore

	// Callbacks and SMS capability state.
	onRegistered     func()
	onSMSReadiness   func(SMSReadiness)
	smsReceiverReady bool
	nextSMSRPMR      byte
	nextSMSConcatRef byte
	smsReassembler   *smscodec.Reassembler
	smsReportTimeout time.Duration

	lastPingAt     time.Time
	securityVerify string

	stop chan struct{}
}

// SMSReadiness describes the independently verifiable IMS SMS prerequisites.
type SMSReadiness struct {
	Registered    bool
	ReceiverReady bool
	SMSCPresent   bool
	Ready         bool
	Reason        string
}

// ServiceStatus is a snapshot of the IMS service state.
type ServiceStatus struct {
	Registered bool
	State      string
	RegState   string
	IMPU       []string
	Domain     string
	LastError  string
}

// IsRegistered reports whether the service is registered.
func (s *ServiceStatus) IsRegistered() bool {
	return s != nil && s.Registered
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
