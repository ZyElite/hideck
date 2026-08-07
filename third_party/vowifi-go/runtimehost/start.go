package runtimehost

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/iniwex5/vowifi-go/engine/ipsec"
	"github.com/iniwex5/vowifi-go/engine/swu"
	"github.com/iniwex5/vowifi-go/internal/vowifi/imscore"
	"github.com/iniwex5/vowifi-go/internal/vowifi/netstack"
	"github.com/iniwex5/vowifi-go/internal/vowifi/profile"
	"github.com/iniwex5/vowifi-go/internal/vowifi/runtimecore"
	"github.com/iniwex5/vowifi-go/runtimehost/eventhost"
	"github.com/iniwex5/vowifi-go/runtimehost/identity"
	"github.com/iniwex5/vowifi-go/runtimehost/messaging"
	"github.com/iniwex5/vowifi-go/runtimehost/voicehost"
)

const (
	imsRegistrationTimeout = 30 * time.Second
	defaultIMSSIPPort      = 5060
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
	IMSFactory    IMSFactory
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
	inst.markTunnelReadyForIMS()
	ims, err := newIMS(req, tunnel)
	if err != nil {
		return failIMSStart(inst, err)
	}
	inst.setService(ims)
	wireSMSReadiness(inst, ims)
	registrationCtx, registrationCancel := context.WithTimeout(runCtx, imsRegistrationTimeout)
	err = ims.Register(registrationCtx)
	registrationCancel()
	if err != nil {
		return failIMSStart(inst, fmt.Errorf("runtimehost: IMS registration failed: %w", err))
	}
	inst.markIMSRegistered()
	syncSMSReadiness(inst, ims)
	go monitorRegistrationFailures(runCtx, inst, ims)
	go stopRuntimeOnContext(runCtx, inst)
	return inst, nil
}

func wireSMSReadiness(inst *Instance, ims IMSLifecycle) {
	source, ok := ims.(smsReadinessSource)
	if !ok {
		return
	}
	source.SetOnSMSReadinessChanged(func(readiness SMSReadiness) {
		inst.updateSMSReadiness(readiness)
	})
}

func syncSMSReadiness(inst *Instance, ims IMSLifecycle) {
	if source, ok := ims.(smsReadinessSource); ok {
		inst.updateSMSReadiness(source.SMSReadiness())
	}
}

func monitorRegistrationFailures(ctx context.Context, inst *Instance, ims IMSLifecycle) {
	source, ok := ims.(registrationFailureSource)
	if !ok || source.RegistrationErrors() == nil {
		return
	}
	for {
		select {
		case <-ctx.Done():
			return
		case err, ok := <-source.RegistrationErrors():
			if !ok {
				return
			}
			if err != nil {
				inst.setIMSRefreshFailure(err)
			}
		}
	}
}

func failIMSStart(inst *Instance, err error) (*Instance, error) {
	_ = inst.Stop(context.Background())
	inst.setIMSFailure(err)
	return inst, err
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
		APN:     imsAPNFromDomain(prepared.IMSIdentity.Domain),
		Carrier: prepared.CarrierConfig,
	}
}

func imsAPNFromDomain(domain string) string {
	domain = strings.TrimSpace(domain)
	if domain == "" {
		return ""
	}
	apn, _, _ := strings.Cut(domain, ".")
	return strings.TrimSpace(apn)
}

func stopRuntimeOnContext(ctx context.Context, inst *Instance) {
	<-ctx.Done()
	_ = inst.Stop(context.Background())
}

func newIMS(req StartRequest, tunnel Tunnel) (IMSLifecycle, error) {
	if req.IMSFactory != nil {
		ims, err := req.IMSFactory(req, tunnel)
		if err != nil {
			return nil, fmt.Errorf("runtimehost: create IMS service: %w", err)
		}
		if ims == nil {
			return nil, errors.New("runtimehost: create IMS service: nil lifecycle")
		}
		return ims, nil
	}
	svc, err := imscoreFromPrepared(req, tunnel)
	if err != nil {
		return nil, err
	}
	return newServiceAdapter(svc), nil
}

// imscoreFromPrepared builds an imscore.Service from the prepared session.
func imscoreFromPrepared(req StartRequest, tunnel Tunnel) (*imscore.Service, error) {
	ident := req.Prepared.IMSIdentity
	domain := strings.TrimSpace(ident.Domain)
	if domain == "" || strings.TrimSpace(ident.IMPI) == "" {
		return nil, errors.New("runtimehost: prepared IMS identity is incomplete")
	}
	impi := ident.IMPI
	impu := []string{ident.IMPU}
	if strings.TrimSpace(impu[0]) == "" {
		impu = []string{"sip:" + impi}
	}
	inner := tunnel.InnerNetwork()
	innerIP, prefixLen := preferredInnerAddress(inner)
	if innerIP == nil || tunnel.InnerPacketIO() == nil {
		return nil, errors.New("runtimehost: SWu tunnel has no usable inner packet network")
	}
	dns := make([]string, 0, len(inner.DNS))
	for _, server := range inner.DNS {
		dns = append(dns, server.String())
	}
	imsNetwork, err := netstack.NewTunnelNetwork(innerIP, prefixLen, dns, tunnel.InnerPacketIO())
	if err != nil {
		return nil, fmt.Errorf("runtimehost: create IMS tunnel network: %w", err)
	}
	registrar := ""
	if pcscf := preferredPCSCF(inner.PCSCF, innerIP); pcscf != nil {
		registrar = net.JoinHostPort(pcscf.String(), fmt.Sprint(defaultIMSSIPPort))
	}
	registerTemplate, userAgent, err := imsRegisterConfigForPrepared(req.Prepared)
	if err != nil {
		_ = imsNetwork.Close()
		return nil, err
	}
	cfg := &imscore.IMSConfig{
		DeviceID:         req.DeviceID,
		IMEI:             imscore.GenerateRandomIMEIForModel(defaultIMSDeviceModel),
		IMSI:             imsiOf(impi),
		IMPI:             impi,
		IMPU:             impu,
		Domain:           domain,
		SMSC:             strings.TrimSpace(req.Prepared.Profile.SMSC),
		Realm:            domain,
		EPDGAddr:         req.Prepared.EPDGAddr,
		LocalIP:          innerIP,
		Registrar:        registrar,
		Transport:        "udp",
		Expires:          registerTemplate.Expires,
		TraceID:          req.TraceID,
		AKAProvider:      req.SIM.AKAProvider(),
		IMSNetwork:       imsNetwork,
		IPSec3GPPEnabled: true,
		UserAgent:        userAgent,
		CellularNetworkInfo: imscore.GenerateDefaultCellularNetworkInfo(
			req.Prepared.Profile.MCC, req.Prepared.Profile.MNC,
		),
		PAccessNetworkCountry: imscore.CountryISO2FromMCC(req.Prepared.Profile.MCC),
		RegisterTemplate:      registerTemplate,
	}
	if req.DeliveryStore != nil {
		cfg.DeliveryStore = newDeliveryStoreAdapter(req.DeliveryStore)
	}
	svc, err := imscore.New(cfg)
	if err != nil {
		_ = imsNetwork.Close()
		return nil, err
	}
	return svc, nil
}

func preferredPCSCF(servers []net.IP, innerIP net.IP) net.IP {
	wantIPv4 := innerIP.To4() != nil
	for _, server := range servers {
		if (server.To4() != nil) == wantIPv4 {
			return server
		}
	}
	return nil
}

func preferredInnerAddress(inner swu.InnerNetworkConfig) (net.IP, int) {
	if inner.IPv6 != nil {
		return inner.IPv6, inner.IPv6PrefixLen
	}
	return inner.IPv4, inner.PrefixLen
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

// WithTraceID returns a context carrying the given trace id.
func WithTraceID(ctx context.Context, traceID string) context.Context {
	return context.WithValue(ctx, traceIDKey{}, traceID)
}

type traceIDKey struct{}
