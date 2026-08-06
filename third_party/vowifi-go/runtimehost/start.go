package runtimehost

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/iniwex5/vowifi-go/internal/vowifi/imscore"
	"github.com/iniwex5/vowifi-go/runtimehost/eventhost"
	"github.com/iniwex5/vowifi-go/runtimehost/identity"
	"github.com/iniwex5/vowifi-go/runtimehost/messaging"
	"github.com/iniwex5/vowifi-go/runtimehost/voicehost"
)

// StartMode selects the runtime host mode.
type StartMode string

const (
	// StartModeMain runs the full IMS host.
	StartModeMain StartMode = "main"
	// StartModeReader runs a SIM-reader-only host.
	StartModeReader StartMode = "reader"
)

// DeliveryStore persists SMS delivery state.
type DeliveryStore interface {
	// (recovered as needed)
}

// EventDispatcher dispatches runtime events.
type EventDispatcher interface {
	Dispatch(event Event)
}

// StartRequest configures a runtime host start.
type StartRequest struct {
	Mode          StartMode
	DeviceID      string
	TraceID       string
	Profile       identity.Profile
	Prepared      *identity.PreparedSession
	NetworkMode   string
	VoiceGateway  *voicehost.Gateway
	SIM           SIMAdapter
	Access        ModemAccessAdapter
	Dataplane     DataplanePolicy
	Proxy         *ProxyConfig
	DeliveryStore messaging.DeliveryStore
	Dispatch      eventhost.Dispatcher
	BeforeStart   func(context.Context, SessionConfig) error
	ShouldRun     func() bool
}

// ModemAccessAdapter is the modem access surface handed to the runtime host.
type ModemAccessAdapter interface {
	Capabilities() ModemCapabilities
	IMSIdentityProvider() IMSIdentityProvider
}

// NewModemAccessAdapter adapts a Modem to the ModemAccessAdapter surface.
func NewModemAccessAdapter(m Modem) ModemAccessAdapter {
	if m == nil {
		return nil
	}
	return modemAccessAdapter{modem: m}
}

// Start launches a runtime host for the given request and returns the Instance.
func Start(ctx context.Context, req StartRequest) (*Instance, error) {
	if req.ShouldRun != nil && !req.ShouldRun() {
		return nil, errors.New("runtimehost: start cancelled by ShouldRun")
	}
	if req.BeforeStart != nil {
		if err := req.BeforeStart(ctx, SessionConfig{}); err != nil {
			return nil, err
		}
	}

	inst := &Instance{}
	inst.setState(State{SessionState: "starting"})

	// Wire the IMS service: build a real imscore service from the prepared
	// identity when available, otherwise fall back to the placeholder.
	inst.setService(buildService(req))

	inst.setState(State{SessionState: "established", EPDGAddress: epdgOf(req)})
	return inst, nil
}

// buildService builds the IMS service for the request: a real imscore
// service when the prepared identity is present, otherwise the placeholder.
func buildService(req StartRequest) Service {
	if req.Prepared != nil && req.Prepared.IMSIdentity.IMPI != "" {
		svc, err := imscoreFromPrepared(req)
		if err == nil && svc != nil {
			return newServiceAdapter(svc)
		}
	}
	return newHostService(req)
}

// imscoreFromPrepared builds an imscore.Service from the prepared session.
func imscoreFromPrepared(req StartRequest) (*imscore.Service, error) {
	ident := req.Prepared.IMSIdentity
	domain := ident.Domain
	if domain == "" {
		domain = "ims.mnc000.mcc000.3gppnetwork.org"
	}
	impi := ident.IMPI
	impu := []string{ident.IMPU}
	if len(impu) == 0 || impu[0] == "" {
		impu = []string{"sip:" + impi}
	}
	cfg := &imscore.IMSConfig{
		DeviceID:  req.DeviceID,
		IMSI:      imsiOf(impi),
		IMPI:      impi,
		IMPU:      impu,
		Domain:    domain,
		Realm:     domain,
		EPDGAddr:  req.Prepared.EPDGAddr,
		Transport: "udp",
		Expires:   3600 * time.Second,
		TraceID:   req.TraceID,
	}
	if req.DeliveryStore != nil {
		cfg.DeliveryStore = newDeliveryStoreAdapter(req.DeliveryStore)
	}
	return imscore.New(cfg)
}

// imsiOf extracts the IMSI from an IMPI (the part before '@').
func imsiOf(impi string) string {
	if i := strings.IndexByte(impi, '@'); i > 0 {
		return impi[:i]
	}
	return impi
}

// epdgOf returns the ePDG address from the prepared session.
func epdgOf(req StartRequest) string {
	if req.Prepared != nil {
		return req.Prepared.EPDGAddr
	}
	return ""
}

// newHostService builds the IMS service for the request. The full service
// (SMS/USSD over IMS) is wired once the imscore/voice engine is complete.
func newHostService(req StartRequest) Service {
	return &hostService{req: req}
}

// hostService is a minimal Service implementation that carries the start
// request until the real IMS service is wired in.
type hostService struct {
	req StartRequest
}

func (s *hostService) SendSMSWithOptions(ctx context.Context, to, text string, opts messaging.SendOptions) (messaging.SendOutcome, error) {
	return messaging.SendOutcome{}, errors.New("runtimehost: SMS service not available")
}
func (s *hostService) SendSMSWithResult(ctx context.Context, to, text string) (messaging.SendOutcome, error) {
	return messaging.SendOutcome{}, errors.New("runtimehost: SMS service not available")
}
func (s *hostService) GetSMSDeliveryStatus(ctx context.Context, ref string) (*messaging.DeliveryStatus, error) {
	return nil, errors.New("runtimehost: SMS service not available")
}
func (s *hostService) SendUSSD(ctx context.Context, code string) (*messaging.USSDResult, error) {
	return nil, errors.New("runtimehost: USSD service not available")
}
func (s *hostService) ContinueUSSD(ctx context.Context, sessionID, input string) (*messaging.USSDResult, error) {
	return nil, errors.New("runtimehost: USSD service not available")
}
func (s *hostService) CancelUSSD(ctx context.Context, sessionID string) error { return errors.New("runtimehost: USSD service not available") }
func (s *hostService) Status() Status    { return Status{} }
func (s *hostService) StatusSnapshot() Status { return Status{} }
func (s *hostService) Stop()              {}
func (s *hostService) TriggerRegisterImmediate() {}

// WithTraceID returns a context carrying the given trace id.
func WithTraceID(ctx context.Context, traceID string) context.Context {
	return context.WithValue(ctx, traceIDKey{}, traceID)
}

type traceIDKey struct{}
