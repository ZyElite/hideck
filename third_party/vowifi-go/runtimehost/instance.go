package runtimehost

import (
	"context"
	"crypto/rand"
	"encoding/hex"
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
	i.mu.Lock()
	if i.stopped {
		i.mu.Unlock()
		return nil
	}
	i.stopped = true
	svc := i.service
	i.mu.Unlock()
	if svc != nil {
		svc.Stop()
	}
	return nil
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
	i.mu.Lock()
	i.state = s
	i.mu.Unlock()
	i.publish(Event{Type: "state", Detail: s.SessionState, Session: i})
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
	svc := i.Service()
	if svc == nil {
		return errNoService
	}
	svc.TriggerRegisterImmediate()
	return nil
}

// newTraceID returns a random hex trace id.
func newTraceID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
