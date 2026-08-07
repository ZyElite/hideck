// Package runtimehost is the IMS runtime host: it owns the VoWiFi session
// lifecycle, exposes SMS/USSD services and publishes runtime events.
//
// Reconstructed from the decompiled engine/runtimehost. The Instance is the
// central object the vohive host wires the modem, SIM and IMS service into.
package runtimehost

import (
	"context"
	"errors"
	"sync"
	"time"

	enginesim "github.com/iniwex5/vowifi-go/engine/sim"
	"github.com/iniwex5/vowifi-go/engine/swu"
	"github.com/iniwex5/vowifi-go/runtimehost/identity"
	"github.com/iniwex5/vowifi-go/runtimehost/messaging"
)

// State is the runtime state of the IMS host (session + IMS registration).
type State struct {
	// Session state (e.g. "idle", "connecting", "established", "error").
	SessionState string
	// IMS registration state.
	IMSState string
	// Error text when the session is in an error state.
	Error string
	// Whether the data plane is up.
	DataPlaneUp bool
	// Whether the session is behind NAT.
	NATDetected bool
	// The negotiated ePDG address.
	EPDGAddress string
	// Network mode at startup.
	NetworkMode string
	// Runtime phase (e.g. PhaseSIMReady).
	Phase string
	// Last state update time.
	UpdatedAt time.Time
	// Last error class (e.g. "auth", "network").
	LastErrorClass string
	// Registration status code (modem convention: 1 = registered).
	RegStatus int
	// Registration status text.
	RegStatusText string
	// Last error text.
	LastError string
	// Last failure reason.
	LastReason string
	// Device id.
	DeviceID string
	// Data plane mode.
	DataplaneMode string
	// Whether the SIM is ready.
	SIMReady bool
	// Whether the modem access is ready.
	AccessReady bool
	// Whether the tunnel is ready.
	TunnelReady bool
	// Whether IMS is ready.
	IMSReady bool
	// Whether SMS is ready.
	SMSReady bool
}

// Event is a runtime event published to observers.
type Event struct {
	Type    string
	Detail  string
	State   State
	Session *Instance
}

// ObserverFunc receives runtime events.
type ObserverFunc func(ctx context.Context, ev Event)

// Notifier receives session notifications (e.g. SMS/USSD events).
type Notifier func(msg string)

// SMSNotifier receives SMS delivery notifications.
type SMSNotifier func(deviceID, sender, content string, ts time.Time)

// Service is the IMS service surface exposed by the runtime host.
type Service interface {
	SendSMSWithOptions(ctx context.Context, to, text string, opts messaging.SendOptions) (messaging.SendOutcome, error)
	SendSMSWithResult(ctx context.Context, to, text string) (messaging.SendOutcome, error)
	GetSMSDeliveryStatus(ctx context.Context, ref string) (*messaging.DeliveryStatus, error)
	SendUSSD(ctx context.Context, code string) (*messaging.USSDResult, error)
	ContinueUSSD(ctx context.Context, sessionID, input string) (*messaging.USSDResult, error)
	CancelUSSD(ctx context.Context, sessionID string) error
	Status() Status
	StatusSnapshot() Status
	Stop()
	TriggerRegisterImmediate()
}

// SendOptions carries optional SMS delivery parameters.
type SendOptions = messaging.SendOptions

// SendOutcome is the result of an SMS send.
type SendOutcome = messaging.SendOutcome

// DeliveryStatus is the delivery status of an SMS.
type DeliveryStatus = messaging.DeliveryStatus

// USSDResult is the result of a USSD operation.
type USSDResult = messaging.USSDResult

// Status is a snapshot of the runtime host status.
type Status struct {
	State State
}

// Modem is the modem access surface the runtime host drives.
type Modem interface {
	Capabilities() ModemCapabilities
	IMSIdentityProvider() IMSIdentityProvider
	GetNetworkMode() string
	// GetRegStatus returns the registration status code and text. The code
	// follows the vohive modem convention (1 = registered).
	GetRegStatus() (int, string)
}

// ModemCapabilities describes what the modem supports.
type ModemCapabilities struct {
	// (recovered as needed)
}

// IMSIdentityProvider reads the IMS identity from the SIM.
type IMSIdentityProvider = identity.IMSIdentityProvider

// Identity is an IMS identity (IMSI/IMPI/IMPU).
type Identity struct {
	IMSI string
	IMPI string
	IMPU string
}

// SIMAdapter is the SIM access surface.
type SIMAdapter interface {
	AKAProvider() enginesim.AKAProvider
}

// Tunnel is the SWu session lifecycle owned by a runtime Instance.
type Tunnel interface {
	Connect(context.Context) error
	Shutdown()
	State() string
	WaitDoneContext(context.Context) error
	InnerNetwork() swu.InnerNetworkConfig
	InnerPacketIO() swu.InnerPacketIO
}

// IMSLifecycle is the registered IMS service owned by the runtime Instance.
type IMSLifecycle interface {
	Service
	Register(context.Context) error
}

type registrationFailureSource interface {
	RegistrationErrors() <-chan error
}

// IMSFactory builds the IMS lifecycle after SWu has established.
type IMSFactory func(StartRequest, Tunnel) (IMSLifecycle, error)

// TunnelFactory builds an SWu tunnel from the prepared configuration.
// Tests and alternate hosts can inject the transport boundary explicitly.
type TunnelFactory func(*swu.Config) (Tunnel, error)

// ProxyConfig configures the ePDG proxy path.
type ProxyConfig struct {
	ID       string
	Addr     string
	Username string
	Password string
	Enabled  bool
}

// SessionConfig configures the SWu session.
type SessionConfig struct {
	DataplaneMode string
}

// DataplanePolicy selects the data-plane implementation.
type DataplanePolicy struct {
	Mode string
}

// ErrAPDUBusy is returned when the SIM APDU channel is busy.
var ErrAPDUBusy = errors.New("runtimehost: APDU channel busy")

// errNoService is returned when an operation requires a service that has not
// been installed on the instance.
var errNoService = errors.New("runtimehost: no service installed")

// PhaseSIMReady is the runtime phase after the SIM is ready.
const PhaseSIMReady = "sim_ready"

// Instance is the IMS runtime host.
type Instance struct {
	mu          sync.RWMutex
	state       State
	service     Service
	tunnel      Tunnel
	cancel      context.CancelFunc
	observers   []ObserverFunc
	notifier    Notifier
	smsNotifier SMSNotifier
	stopped     bool
}

// NewTraceID returns a fresh trace id.
func NewTraceID() string {
	return newTraceID()
}

// The adapter types below bridge the runtime host to the modem, SIM, identity
// and delivery-store surfaces. They are implemented in adapters.go.

type accessAdapter struct {
	access IMSIdentityProvider
}

// Capabilities reports the identity capabilities.
func (a *accessAdapter) Capabilities() ModemCapabilities {
	return ModemCapabilities{}
}

// IMSIdentityProvider returns the underlying identity provider.
func (a *accessAdapter) IMSIdentityProvider() IMSIdentityProvider {
	if a == nil {
		return nil
	}
	return a.access
}

type identityProviderAdapter struct {
	provider IMSIdentityProvider
}

// GetISIMIdentity reads the ISIM identity.
func (a *identityProviderAdapter) GetISIMIdentity() (Identity, error) {
	if a == nil || a.provider == nil {
		return Identity{}, errNoIdentityProvider
	}
	p, ok := a.provider.(interface {
		GetISIMIdentity() (identity.Identity, error)
	})
	if !ok {
		return Identity{}, errNoIdentityProvider
	}
	ident, err := p.GetISIMIdentity()
	if err != nil {
		return Identity{}, err
	}
	return Identity{IMSI: ident.IMSI, IMPI: ident.IMPI}, nil
}

// errNoIdentityProvider is returned when no identity provider is installed.
var errNoIdentityProvider = errors.New("runtimehost: no identity provider")

type modemAccessAdapter struct {
	modem Modem
}

// Capabilities returns the modem capabilities.
func (a modemAccessAdapter) Capabilities() ModemCapabilities {
	if a.modem == nil {
		return ModemCapabilities{}
	}
	return a.modem.Capabilities()
}

// IMSIdentityProvider returns the modem's IMS identity provider.
func (a modemAccessAdapter) IMSIdentityProvider() IMSIdentityProvider {
	if a.modem == nil {
		return nil
	}
	return a.modem.IMSIdentityProvider()
}
