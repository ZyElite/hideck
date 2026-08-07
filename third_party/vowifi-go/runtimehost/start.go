package runtimehost

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/iniwex5/vowifi-go/engine/ipsec"
	"github.com/iniwex5/vowifi-go/engine/swu"
	"github.com/iniwex5/vowifi-go/internal/vowifi/imscore"
	"github.com/iniwex5/vowifi-go/internal/vowifi/profile"
	"github.com/iniwex5/vowifi-go/internal/vowifi/runtimecore"
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
	TunnelFactory TunnelFactory
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
	if ctx == nil {
		ctx = context.Background()
	}
	if req.ShouldRun != nil && !req.ShouldRun() {
		return nil, errors.New("runtimehost: start cancelled by ShouldRun")
	}
	if err := validateStartRequest(req); err != nil {
		return nil, err
	}
	if req.BeforeStart != nil {
		if err := req.BeforeStart(ctx, SessionConfig{DataplaneMode: req.Dataplane.Mode}); err != nil {
			return nil, err
		}
	}

	inst := &Instance{}
	inst.setState(initialState(req))
	runCtx, cancel := context.WithCancel(ctx)
	tunnel, err := newTunnel(req, inst)
	if err != nil {
		cancel()
		inst.setStartFailure(err)
		return inst, err
	}
	inst.attachTunnel(tunnel, cancel)
	if err := tunnel.Connect(runCtx); err != nil {
		startErr := fmt.Errorf("runtimehost: SWu tunnel establishment failed: %w", err)
		_ = inst.Stop(context.Background())
		inst.setStartFailure(startErr)
		return inst, startErr
	}
	inst.markTunnelEstablished()
	inst.setService(buildService(req))
	go stopRuntimeOnContext(runCtx, inst)
	return inst, nil
}

func validateStartRequest(req StartRequest) error {
	if strings.TrimSpace(req.DeviceID) == "" {
		return errors.New("runtimehost: device_id is empty")
	}
	if req.Prepared == nil {
		return errors.New("runtimehost: prepared session is required")
	}
	if strings.TrimSpace(req.Prepared.EPDGAddr) == "" {
		return errors.New("runtimehost: prepared session has no ePDG address")
	}
	if req.SIM == nil || req.SIM.AKAProvider() == nil {
		return errors.New("runtimehost: SIM AKA provider is required")
	}
	mode := strings.TrimSpace(req.Dataplane.Mode)
	if mode != "" && mode != swu.DataplaneModeUserspace {
		return fmt.Errorf("runtimehost: unsupported dataplane mode %q", mode)
	}
	if req.Proxy != nil && req.Proxy.Enabled && strings.TrimSpace(req.Proxy.Addr) == "" {
		return errors.New("runtimehost: enabled SOCKS5 proxy has no address")
	}
	return nil
}

func initialState(req StartRequest) State {
	return State{
		SessionState: "starting",
		DeviceID:     req.DeviceID, EPDGAddress: epdgOf(req), NetworkMode: req.NetworkMode,
		DataplaneMode: req.Dataplane.Mode, SIMReady: true, AccessReady: req.Access != nil,
	}
}

func newTunnel(req StartRequest, inst *Instance) (Tunnel, error) {
	prepared := preparedForRuntimeCore(req.Prepared)
	cfg, err := runtimecore.BuildSWUConfig(prepared, req.SIM.AKAProvider())
	if err != nil {
		return nil, err
	}
	cfg.OnStateChange = inst.updateTunnelState
	if req.Proxy != nil && req.Proxy.Enabled {
		cfg.ProxyAddr = strings.TrimSpace(req.Proxy.Addr)
		cfg.Proxy = &ipsec.Socks5Config{Username: req.Proxy.Username, Password: req.Proxy.Password}
	}
	factory := req.TunnelFactory
	if factory == nil {
		factory = func(cfg *swu.Config) (Tunnel, error) { return swu.NewSession(cfg), nil }
	}
	tunnel, err := factory(cfg)
	if err != nil {
		return nil, fmt.Errorf("runtimehost: create SWu tunnel: %w", err)
	}
	if tunnel == nil {
		return nil, errors.New("runtimehost: create SWu tunnel: nil session")
	}
	return tunnel, nil
}

func preparedForRuntimeCore(prepared *identity.PreparedSession) *runtimecore.PreparedSessionStart {
	return &runtimecore.PreparedSessionStart{
		Profile: profile.Profile{
			IMSI: prepared.Profile.IMSI, MCC: prepared.Profile.MCC, MNC: prepared.Profile.MNC,
			IMEI: prepared.Profile.IMEI, SMSC: prepared.Profile.SMSC,
		},
		IMSIdentity: profile.IMSIdentity{
			IMPI: prepared.IMSIdentity.IMPI, IMPU: []string{prepared.IMSIdentity.IMPU},
			Domain: prepared.IMSIdentity.Domain,
		},
		AuthPlan: profile.AuthPlan{AKAApp: profile.NormalizeAKAApp(string(prepared.IMSIdentity.AKAAppPreference))},
		EPDGAddr: prepared.EPDGAddr, EPDGSource: prepared.EPDGSource,
	}
}

func stopRuntimeOnContext(ctx context.Context, inst *Instance) {
	<-ctx.Done()
	_ = inst.Stop(context.Background())
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
func (s *hostService) CancelUSSD(ctx context.Context, sessionID string) error {
	return errors.New("runtimehost: USSD service not available")
}
func (s *hostService) Status() Status            { return Status{} }
func (s *hostService) StatusSnapshot() Status    { return Status{} }
func (s *hostService) Stop()                     {}
func (s *hostService) TriggerRegisterImmediate() {}

// WithTraceID returns a context carrying the given trace id.
func WithTraceID(ctx context.Context, traceID string) context.Context {
	return context.WithValue(ctx, traceIDKey{}, traceID)
}

type traceIDKey struct{}
