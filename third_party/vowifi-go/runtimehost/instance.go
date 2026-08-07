package runtimehost

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"time"

	"github.com/iniwex5/vowifi-go/runtimehost/messaging"
)

// State returns a copy of the current runtime state.
func (i *Instance) State() State {
	i.mu.RLock()
	defer i.mu.RUnlock()
	return i.state
}

// Service returns the current IMS service (nil until set).
func (i *Instance) Service() Service {
	i.mu.RLock()
	defer i.mu.RUnlock()
	return i.service
}

// AddObserver registers an observer that receives runtime events.
func (i *Instance) AddObserver(obs ObserverFunc) {
	i.mu.Lock()
	defer i.mu.Unlock()
	i.observers = append(i.observers, obs)
}

// SetNotifier installs the session notifier.
func (i *Instance) SetNotifier(n Notifier) {
	i.mu.Lock()
	i.notifier = n
	i.mu.Unlock()
}

// SetSMSNotifier installs the SMS notifier.
func (i *Instance) SetSMSNotifier(n SMSNotifier) {
	i.mu.Lock()
	i.smsNotifier = n
	i.mu.Unlock()
}

// Stop shuts the runtime host down.
func (i *Instance) Stop(ctx context.Context) error {
	if i == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	i.mu.Lock()
	if i.stopped {
		i.mu.Unlock()
		return nil
	}
	i.stopped = true
	svc := i.service
	tunnel := i.tunnel
	cancel := i.cancel
	i.tunnel = nil
	i.cancel = nil
	i.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	if tunnel != nil {
		tunnel.Shutdown()
	}
	if svc != nil {
		svc.Stop()
	}
	i.updateState(func(state *State) {
		state.SessionState = "stopped"
		state.DataPlaneUp = false
		state.TunnelReady = false
		state.IMSReady = false
		state.SMSReady = false
		state.LastReason = "stopped"
	})
	if tunnel == nil {
		return nil
	}
	return tunnel.WaitDoneContext(ctx)
}

// StopShared stops the host without tearing down shared resources.
func (i *Instance) StopShared(ctx context.Context) error {
	return i.Stop(ctx)
}

// RuntimeState returns the current runtime state.
func (i *Instance) RuntimeState() State {
	return i.State()
}

// Status returns a human-readable status string.
func (i *Instance) Status() string {
	st := i.State()
	if st.Error != "" {
		return "VoWiFi: " + st.Error
	}
	if st.SessionState == "" {
		return "VoWiFi: STOPPED"
	}
	return "VoWiFi: " + st.SessionState
}

// Obs returns an observation map of the runtime state.
func (i *Instance) Obs() map[string]interface{} {
	st := i.State()
	return map[string]interface{}{
		"session_state": st.SessionState,
		"ims_state":     st.IMSState,
		"epdg":          st.EPDGAddress,
		"nat":           st.NATDetected,
	}
}

// setState updates the runtime state and publishes the change.
func (i *Instance) setState(s State) {
	s.UpdatedAt = time.Now()
	i.mu.Lock()
	i.state = s
	i.mu.Unlock()
	i.publish(Event{Type: "state", Detail: s.SessionState, State: s, Session: i})
}

func (i *Instance) updateState(update func(*State)) {
	i.mu.Lock()
	update(&i.state)
	i.state.UpdatedAt = time.Now()
	state := i.state
	i.mu.Unlock()
	i.publish(Event{Type: "state", Detail: state.SessionState, State: state, Session: i})
}

func (i *Instance) attachTunnel(tunnel Tunnel, cancel context.CancelFunc) {
	i.mu.Lock()
	i.tunnel = tunnel
	i.cancel = cancel
	i.mu.Unlock()
}

func (i *Instance) updateTunnelState(sessionState string) {
	i.updateState(func(state *State) {
		state.SessionState = sessionState
		state.TunnelReady = sessionState == "established"
		state.DataPlaneUp = state.TunnelReady
		if sessionState == "error" || sessionState == "shutdown" {
			state.IMSState = "failed"
			state.IMSReady = false
			state.SMSReady = false
			state.RegStatus = 0
			state.RegStatusText = "failed"
		}
	})
}

func (i *Instance) markTunnelReadyForIMS() {
	i.updateState(func(state *State) {
		state.SessionState = "registering"
		state.IMSState = "registering"
		state.TunnelReady = true
		state.DataPlaneUp = true
		state.LastReason = "SWu tunnel established; registering IMS"
	})
}

func (i *Instance) markIMSRegistered() {
	i.updateState(func(state *State) {
		state.SessionState = "established"
		state.IMSState = "registered"
		state.IMSReady = true
		state.SMSReady = false
		state.SMSReadyReason = "IMS SMS readiness has not been reported"
		state.RegStatus = 1
		state.RegStatusText = "registered"
		state.LastReason = "IMS registered"
	})
}

func (i *Instance) updateSMSReadiness(readiness SMSReadiness) {
	i.updateState(func(state *State) {
		state.SMSReady = state.IMSReady && readiness.Ready
		state.SMSReadyReason = readiness.Reason
	})
}

func (i *Instance) setStartFailure(err error) {
	i.updateState(func(state *State) {
		state.SessionState = "error"
		state.Error = err.Error()
		state.LastError = err.Error()
		state.LastErrorClass = "network"
		state.LastReason = "SWu tunnel establishment failed"
		state.TunnelReady = false
		state.DataPlaneUp = false
	})
}

func (i *Instance) setIMSFailure(err error) {
	i.updateState(func(state *State) {
		state.SessionState = "error"
		state.IMSState = "failed"
		state.Error = err.Error()
		state.LastError = err.Error()
		state.LastErrorClass = "ims"
		state.LastReason = "IMS registration failed"
		state.TunnelReady = false
		state.DataPlaneUp = false
		state.IMSReady = false
		state.SMSReady = false
		state.RegStatus = 0
		state.RegStatusText = "failed"
	})
}

func (i *Instance) setIMSRefreshFailure(err error) {
	i.updateState(func(state *State) {
		state.SessionState = "error"
		state.IMSState = "failed"
		state.Error = err.Error()
		state.LastError = err.Error()
		state.LastErrorClass = "ims"
		state.LastReason = "IMS registration refresh failed"
		state.IMSReady = false
		state.SMSReady = false
		state.RegStatus = 0
		state.RegStatusText = "failed"
	})
}

func (i *Instance) setTunnelControlFailure(err error) {
	i.updateState(func(state *State) {
		state.SessionState = "error"
		state.IMSState = "failed"
		state.Error = err.Error()
		state.LastError = err.Error()
		state.LastErrorClass = "network"
		state.LastReason = "SWu tunnel control failed"
		state.TunnelReady = false
		state.DataPlaneUp = false
		state.IMSReady = false
		state.SMSReady = false
		state.RegStatus = 0
		state.RegStatusText = "failed"
	})
}

// setService installs the IMS service.
func (i *Instance) setService(s Service) {
	i.mu.Lock()
	i.service = s
	i.mu.Unlock()
}

// setSession wires a new session into the host.
func (i *Instance) setSession(s Service) {
	i.setService(s)
}

// publish delivers an event to all observers and the notifier.
func (i *Instance) publish(ev Event) {
	i.mu.RLock()
	obs := append([]ObserverFunc{}, i.observers...)
	notifier := i.notifier
	smsNotifier := i.smsNotifier
	i.mu.RUnlock()
	for _, o := range obs {
		if o != nil {
			o(context.Background(), ev)
		}
	}
	if notifier != nil {
		notifier(ev.Detail)
	}
	if smsNotifier != nil {
		smsNotifier("", "", ev.Detail, time.Now())
	}
}

// --- SMS/USSD service delegation ---

// SendSMSWithResult sends an SMS and returns the delivery outcome.
func (i *Instance) SendSMSWithResult(ctx context.Context, to, text string) (messaging.SendOutcome, error) {
	svc := i.Service()
	if svc == nil {
		return messaging.SendOutcome{}, errNoService
	}
	return svc.SendSMSWithResult(ctx, to, text)
}

// SendSMSWithOptions sends an SMS with delivery options.
func (i *Instance) SendSMSWithOptions(ctx context.Context, to, text string, opts messaging.SendOptions) (messaging.SendOutcome, error) {
	svc := i.Service()
	if svc == nil {
		return messaging.SendOutcome{}, errNoService
	}
	return svc.SendSMSWithOptions(ctx, to, text, opts)
}

// GetSMSDeliveryStatus returns the delivery status of a previously sent SMS.
func (i *Instance) GetSMSDeliveryStatus(ctx context.Context, ref string) (*messaging.DeliveryStatus, error) {
	svc := i.Service()
	if svc == nil {
		return nil, errNoService
	}
	return svc.GetSMSDeliveryStatus(ctx, ref)
}

// SendUSSD sends a USSD request.
func (i *Instance) SendUSSD(ctx context.Context, code string) (*messaging.USSDResult, error) {
	svc := i.Service()
	if svc == nil {
		return nil, errNoService
	}
	return svc.SendUSSD(ctx, code)
}

// ContinueUSSD continues a USSD session.
func (i *Instance) ContinueUSSD(ctx context.Context, sessionID, input string) (*messaging.USSDResult, error) {
	svc := i.Service()
	if svc == nil {
		return nil, errNoService
	}
	return svc.ContinueUSSD(ctx, sessionID, input)
}

// CancelUSSD cancels a USSD session.
func (i *Instance) CancelUSSD(ctx context.Context, sessionID string) error {
	svc := i.Service()
	if svc == nil {
		return errNoService
	}
	return svc.CancelUSSD(ctx, sessionID)
}

// TriggerMOBIKE forces a MOBIKE update on the session after an address change.
func (i *Instance) TriggerMOBIKE(oldIP, newIP string) error {
	oldAddress := net.ParseIP(oldIP)
	newAddress := net.ParseIP(newIP)
	if oldAddress == nil || newAddress == nil {
		return errors.New("runtimehost: MOBIKE requires valid old and new IP addresses")
	}
	i.mu.RLock()
	tunnel := i.tunnel
	i.mu.RUnlock()
	if tunnel == nil {
		return errors.New("runtimehost: no SWu tunnel installed")
	}
	if err := tunnel.UpdateAddresses(oldAddress, newAddress); err != nil {
		wrapped := fmt.Errorf("runtimehost: MOBIKE address update failed: %w", err)
		i.setTunnelControlFailure(wrapped)
		return wrapped
	}
	return nil
}

// newTraceID returns a random hex trace id.
func newTraceID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
