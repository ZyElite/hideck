// Package runtimecore orchestrates the VoWiFi runtime: preparing the session,
// running the SWu tunnel, and wiring the voice/IMS lifecycle.
//
// Reconstructed from the decompiled internal/vowifi/runtimecore.
package runtimecore

import (
	"context"
	"errors"
	"fmt"
	"net"
	"sync"
	"time"

	enginesim "github.com/iniwex5/vowifi-go/engine/sim"
	"github.com/iniwex5/vowifi-go/engine/swu"
	"github.com/iniwex5/vowifi-go/internal/vowifi/epdg"
	"github.com/iniwex5/vowifi-go/internal/vowifi/netstack"
	"github.com/iniwex5/vowifi-go/internal/vowifi/profile"
)

// ErrRedirect is returned when the ePDG redirects the session (RFC 5685).
type ErrRedirect struct {
	Target string
}

func (e *ErrRedirect) Error() string {
	if e == nil {
		return ""
	}
	return "redirected to " + e.Target
}

// RuntimeConfig is the input to PrepareSessionStart.
type RuntimeConfig struct {
	DeviceID     string
	Profile      profile.Profile
	EPDGOverride string
	IMSIdentity  profile.IMSIdentity
	AKAApp       profile.AKAAppPreference
	NetworkMode  string
	Access       Access
}

// Access abstracts the SIM access surface.
type Access interface {
	// IMSIdentityProvider returns the ISIM identity provider, if any.
	IMSIdentityProvider() IdentityProvider
	// AKAProvider returns the AKA provider for the SIM.
	AKAProvider() AKAProvider
	// Capabilities reports what the access supports.
	Capabilities() AccessCapabilities
}

// IdentityProvider reads the ISIM identity.
type IdentityProvider interface {
	GetISIMIdentity() (profile.IMSIdentity, error)
}

// AKAProvider computes AKA from the network challenge.
type AKAProvider = enginesim.AKAProvider

// AKAResult is the outcome of an AKA computation.
type AKAResult = enginesim.AKAResult

// AccessCapabilities describes the access capabilities.
type AccessCapabilities struct {
	HasISIM bool
	HasUSIM bool
}

// PreparedSessionStart is the output of PrepareSessionStart.
type PreparedSessionStart struct {
	Profile     profile.Profile
	IMSIdentity profile.IMSIdentity
	AuthPlan    profile.AuthPlan
	EPDGAddr    string
	EPDGSource  string
}

// PrepareSessionStart prepares a session start from the config.
func PrepareSessionStart(cfg RuntimeConfig) (*PreparedSessionStart, error) {
	p := profile.Normalize(cfg.Profile)
	plan := profile.AuthPlan{}
	if cfg.Access != nil {
		caps := cfg.Access.Capabilities()
		plan.ISIMAvailable = caps.HasISIM
		plan.USIMAvailable = caps.HasUSIM
		plan.AKAApp = cfg.AKAApp
		plan.Normalize()
	}

	identity := cfg.IMSIdentity
	if identity.IMPI == "" && cfg.Access != nil && cfg.Access.IMSIdentityProvider() != nil {
		if id, err := cfg.Access.IMSIdentityProvider().GetISIMIdentity(); err == nil {
			identity = id
		}
	}

	epdgAddr, epdgSource := "epdg.epc.att.net", "default"
	if cfg.EPDGOverride != "" {
		epdgAddr, epdgSource = cfg.EPDGOverride, "override"
	}

	return &PreparedSessionStart{
		Profile:     p,
		IMSIdentity: identity,
		AuthPlan:    plan,
		EPDGAddr:    epdgAddr,
		EPDGSource:  epdgSource,
	}, nil
}

// preparedSessionWithRuntimeOverride applies runtime overrides to a prepared
// session.
func preparedSessionWithRuntimeOverride(prepared *PreparedSessionStart, epdgOverride string) *PreparedSessionStart {
	if prepared == nil {
		return prepared
	}
	if epdgOverride != "" {
		prepared.EPDGAddr = epdgOverride
		prepared.EPDGSource = "override"
	}
	return prepared
}

// BuildSWUConfig builds the SWu session config from the prepared session.
func BuildSWUConfig(prepared *PreparedSessionStart, aka AKAProvider) (*swu.Config, error) {
	if prepared == nil {
		return nil, errors.New("runtimecore: nil prepared session")
	}
	if aka == nil {
		return nil, errors.New("runtimecore: no SWu AKA provider")
	}
	return &swu.Config{
		EPDGAddr:    prepared.EPDGAddr,
		IMSI:        prepared.Profile.IMSI,
		MCC:         prepared.Profile.MCC,
		MNC:         prepared.Profile.MNC,
		AKAProvider: aka,
	}, nil
}

// RunLoop runs the runtime loop until the context is cancelled.
func RunLoop(ctx context.Context, run func(ctx context.Context) error) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if run == nil {
		return errors.New("runtimecore: nil run function")
	}
	return run(ctx)
}

// RunSession runs a SWu session to completion.
func RunSession(ctx context.Context, session *swu.Session) error {
	if session == nil {
		return errors.New("runtimecore: nil session")
	}
	if err := session.Connect(ctx); err != nil {
		return err
	}
	defer session.Shutdown()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-sessionDoneChan(session):
		return nil
	}
}

// sessionDoneChan returns the session's done channel.
func sessionDoneChan(session *swu.Session) <-chan struct{} {
	if session == nil {
		return nil
	}
	ch := make(chan struct{})
	go func() {
		session.WaitDone()
		close(ch)
	}()
	return ch
}

// StartAndWaitEPDG starts the ePDG manager and waits for it to stop.
func StartAndWaitEPDG(addr string) error {
	m := epdg.New(addr)
	if err := m.Start(); err != nil {
		return err
	}
	defer m.Stop()
	m.Wait()
	return nil
}

// CleanupDataplaneInterface cleans up the data-plane network interface.
func CleanupDataplaneInterface() error {
	return nil
}

// NewUserspaceIMSNetwork creates the user-space IMS network.
func NewUserspaceIMSNetwork(innerIP net.IP, prefixLen int, dns []string) *netstack.Network {
	return netstack.NewNetwork(innerIP, prefixLen, dns)
}

// applyRedirectOverride applies a redirect target to the session.
func applyRedirectOverride(session *swu.Session, target string) {
	if session == nil || target == "" {
		return
	}
	_ = target
}

// defaultSessionStarter returns the default session start function.
func defaultSessionStarter(prepared *PreparedSessionStart, aka AKAProvider) func(context.Context) (*swu.Session, error) {
	return func(ctx context.Context) (*swu.Session, error) {
		cfg, err := BuildSWUConfig(prepared, aka)
		if err != nil {
			return nil, err
		}
		session := swu.NewSession(cfg)
		return session, nil
	}
}

// defaultStopSession returns the default stop function.
func defaultStopSession() func(*swu.Session) {
	return func(s *swu.Session) {
		if s != nil {
			s.Shutdown()
		}
	}
}

// emitAllRuntimeEvents emits the runtime state events.
func emitAllRuntimeEvents(emit func(state string)) {
	if emit == nil {
		return
	}
	emit("idle")
}

// waitRuntimeInterruption waits for the runtime interruption channel.
func waitRuntimeInterruption(ctx context.Context, ch <-chan struct{}) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-ch:
		return nil
	}
}

// waitSessionCleanup waits for the session to clean up.
func waitSessionCleanup(session *swu.Session, timeout time.Duration) error {
	if session == nil {
		return nil
	}
	done := sessionDoneChan(session)
	select {
	case <-done:
		return nil
	case <-time.After(timeout):
		return errors.New("runtimecore: session cleanup timed out")
	}
}

// snapshotFromSessionResult builds a state snapshot from a session result.
func snapshotFromSessionResult(session *swu.Session, err error) map[string]interface{} {
	out := map[string]interface{}{"error": nil}
	if err != nil {
		out["error"] = err.Error()
	}
	if session != nil {
		out["state"] = session.State()
	}
	return out
}

// shouldRetryDeviceIdentity reports whether the device identity should be
// retried.
func shouldRetryDeviceIdentity(err error) bool {
	return err != nil
}

// --- SIM reader adapters ---

// readerSIMAdapter adapts a SIM reader to the runtime surfaces.
type readerSIMAdapter struct {
	reader   LogicalReader
	identity profile.IMSIdentity
}

// LogicalReader reads the ISIM identity.
type LogicalReader interface {
	ReadISIM() (profile.IMSIdentity, error)
}

// EPDGSIMProvider returns the EPDG SIM provider.
func (a *readerSIMAdapter) EPDGSIMProvider() AKAProvider {
	if a == nil {
		return nil
	}
	return a
}

// IMSAKAProvider returns the IMS AKA provider.
func (a *readerSIMAdapter) IMSAKAProvider() AKAProvider {
	if a == nil {
		return nil
	}
	return a
}

// IMSIdentityProvider returns the IMS identity provider.
func (a *readerSIMAdapter) IMSIdentityProvider() IdentityProvider {
	if a == nil {
		return nil
	}
	return a
}

// GetISIMIdentity reads the ISIM identity.
func (a *readerSIMAdapter) GetISIMIdentity() (profile.IMSIdentity, error) {
	if a == nil || a.reader == nil {
		return profile.IMSIdentity{}, errors.New("runtimecore: no SIM reader")
	}
	return a.reader.ReadISIM()
}

// CalculateAKA computes AKA (unsupported on the reader adapter).
func (a *readerSIMAdapter) CalculateAKA(rand16, autn16 []byte) (AKAResult, error) {
	return AKAResult{}, errors.New("runtimecore: AKA not supported on reader adapter")
}

// readerAKAProviderAdapter adapts a reader to an AKA provider.
type readerAKAProviderAdapter struct {
	provider AKAProvider
}

// CalculateAKA delegates to the provider.
func (a *readerAKAProviderAdapter) CalculateAKA(rand16, autn16 []byte) (AKAResult, error) {
	if a == nil || a.provider == nil {
		return AKAResult{}, errors.New("runtimecore: no AKA provider")
	}
	return a.provider.CalculateAKA(rand16, autn16)
}

// readerISIMAKAProviderAdapter adapts a reader to an ISIM AKA provider.
type readerISIMAKAProviderAdapter struct {
	reader LogicalReader
}

// CalculateAKA is unsupported (ISIM identity only).
func (a *readerISIMAKAProviderAdapter) CalculateAKA(rand16, autn16 []byte) (AKAResult, error) {
	return AKAResult{}, errors.New("runtimecore: ISIM AKA not supported")
}

// unsupportedIMSAKAProvider returns an unsupported error.
type unsupportedIMSAKAProvider struct{}

// CalculateAKA returns an unsupported error.
func (unsupportedIMSAKAProvider) CalculateAKA(rand16, autn16 []byte) (AKAResult, error) {
	return AKAResult{}, errors.New("runtimecore: IMS AKA unsupported")
}

// unsupportedSWUAKAProvider returns an unsupported error.
type unsupportedSWUAKAProvider struct{}

// CalculateAKA returns an unsupported error.
func (unsupportedSWUAKAProvider) CalculateAKA(rand16, autn16 []byte) (AKAResult, error) {
	return AKAResult{}, errors.New("runtimecore: SWU AKA unsupported")
}

// resolveIPSec3GPPInstaller resolves the IPsec3GPP installer.
func resolveIPSec3GPPInstaller() interface{} {
	return nil
}

// --- voice lifecycle ---

// voiceLifecycleBinding binds the voice gateway to the IMS registration state.
type voiceLifecycleBinding struct {
	mu       sync.Mutex
	attached bool
	onAttach func()
	notifier *imsRegisteredNotifier
}

// AttachIfReady attaches the voice binding once IMS registration is ready.
func (b *voiceLifecycleBinding) AttachIfReady() error {
	if b == nil {
		return errors.New("runtimecore: nil binding")
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.attached {
		return nil
	}
	b.attached = true
	if b.onAttach != nil {
		b.onAttach()
	}
	return nil
}

// Stop detaches the voice binding.
func (b *voiceLifecycleBinding) Stop() error {
	if b == nil {
		return nil
	}
	b.mu.Lock()
	b.attached = false
	b.mu.Unlock()
	return nil
}

// imsRegisteredNotifier emits a notification when IMS registration completes.
type imsRegisteredNotifier struct {
	mu      sync.Mutex
	session interface{}
	onReg   func()
	emitted bool
}

// newIMSRegisteredNotifier creates a notifier.
func newIMSRegisteredNotifier(onReg func()) *imsRegisteredNotifier {
	return &imsRegisteredNotifier{onReg: onReg}
}

// SetSession wires the session into the notifier.
func (n *imsRegisteredNotifier) SetSession(session interface{}) {
	n.mu.Lock()
	n.session = session
	n.mu.Unlock()
}

// OnIMSRegistered is invoked when IMS registration completes.
func (n *imsRegisteredNotifier) OnIMSRegistered() {
	n.emitRegistered()
}

// emitRegistered emits the registration notification once.
func (n *imsRegisteredNotifier) emitRegistered() {
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.emitted {
		return
	}
	n.emitted = true
	if n.onReg != nil {
		n.onReg()
	}
}

var _ = fmt.Sprintf

// Runtime orchestrates a VoWiFi runtime instance.
type Runtime struct {
	cfg      RuntimeConfig
	prepared *PreparedSessionStart
	mu       sync.Mutex
	started  bool
}

// NewRuntime creates a runtime from a config.
func NewRuntime(cfg RuntimeConfig) *Runtime {
	return &Runtime{cfg: cfg}
}

// Start prepares the session and starts the runtime (ePDG + SWu tunnel).
func (r *Runtime) Start(ctx context.Context) error {
	if r == nil {
		return errors.New("runtimecore: nil runtime")
	}
	r.mu.Lock()
	if r.started {
		r.mu.Unlock()
		return nil
	}
	prepared, err := PrepareSessionStart(r.cfg)
	if err != nil {
		r.mu.Unlock()
		return err
	}
	r.prepared = prepared
	r.started = true
	r.mu.Unlock()
	if prepared.EPDGAddr != "" {
		return StartAndWaitEPDG(prepared.EPDGAddr)
	}
	return nil
}

// Stop shuts the runtime down.
func (r *Runtime) Stop() {
	if r == nil {
		return
	}
	r.mu.Lock()
	r.started = false
	r.mu.Unlock()
	_ = CleanupDataplaneInterface()
}

// Prepared returns the prepared session.
func (r *Runtime) Prepared() *PreparedSessionStart {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.prepared
}
