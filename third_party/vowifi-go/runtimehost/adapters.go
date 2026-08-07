package runtimehost

import (
	"context"
	"errors"
	"time"

	enginesim "github.com/iniwex5/vowifi-go/engine/sim"
	"github.com/iniwex5/vowifi-go/internal/vowifi/imscore"
	"github.com/iniwex5/vowifi-go/runtimehost/identity"
	"github.com/iniwex5/vowifi-go/runtimehost/messaging"
)

// serviceAdapter adapts an imscore.Service to the runtimehost.Service surface.
type serviceAdapter struct {
	svc *imscore.Service
}

// newServiceAdapter wraps an imscore service.
func newServiceAdapter(svc *imscore.Service) *serviceAdapter {
	return &serviceAdapter{svc: svc}
}

// Register runs the real IMS REGISTER flow.
func (a *serviceAdapter) Register(ctx context.Context) error {
	if a == nil || a.svc == nil {
		return errNoService
	}
	return a.svc.Register(ctx)
}

// SendSMSWithOptions sends an SMS with options.
func (a *serviceAdapter) SendSMSWithOptions(ctx context.Context, to, text string, opts messaging.SendOptions) (messaging.SendOutcome, error) {
	if a == nil || a.svc == nil {
		return messaging.SendOutcome{}, errNoService
	}
	out, err := a.svc.SendSMSWithOptions(ctx, to, text, imscore.SMSSendOptions{
		SuppressSendTGSuccess: opts.SuppressSendTGSuccess,
		Encoding:              opts.Encoding,
	})
	if err != nil {
		return messaging.SendOutcome{}, err
	}
	return messaging.SendOutcome{
		Ref:           out.Ref,
		Err:           out.Err,
		MessageID:     out.MessageID,
		PartsTotal:    out.PartsTotal,
		DeliveryState: out.State,
	}, nil
}

// SendSMSWithResult sends an SMS.
func (a *serviceAdapter) SendSMSWithResult(ctx context.Context, to, text string) (messaging.SendOutcome, error) {
	if a == nil || a.svc == nil {
		return messaging.SendOutcome{}, errNoService
	}
	out, err := a.svc.SendSMSWithResult(ctx, to, text)
	if err != nil {
		return messaging.SendOutcome{}, err
	}
	return messaging.SendOutcome{
		Ref:           out.Ref,
		Err:           out.Err,
		MessageID:     out.MessageID,
		PartsTotal:    out.PartsTotal,
		DeliveryState: out.State,
	}, nil
}

// GetSMSDeliveryStatus returns the delivery status of an SMS.
func (a *serviceAdapter) GetSMSDeliveryStatus(ctx context.Context, ref string) (*messaging.DeliveryStatus, error) {
	if a == nil || a.svc == nil {
		return nil, errNoService
	}
	st, err := a.svc.GetSMSDeliveryStatus(ctx, ref)
	if err != nil {
		return nil, err
	}
	return deliveryStatusFromInternal(st), nil
}

// SendUSSD sends a USSD request.
func (a *serviceAdapter) SendUSSD(ctx context.Context, code string) (*messaging.USSDResult, error) {
	if a == nil || a.svc == nil {
		return nil, errNoService
	}
	res, err := a.svc.SendUSSD(ctx, code)
	if err != nil {
		return nil, err
	}
	return &messaging.USSDResult{Code: res.Code, Message: res.Message}, nil
}

// ContinueUSSD continues a USSD session.
func (a *serviceAdapter) ContinueUSSD(ctx context.Context, sessionID, input string) (*messaging.USSDResult, error) {
	if a == nil || a.svc == nil {
		return nil, errNoService
	}
	res, err := a.svc.ContinueUSSD(ctx, sessionID, input)
	if err != nil {
		return nil, err
	}
	return &messaging.USSDResult{Code: res.Code, Message: res.Message}, nil
}

// CancelUSSD cancels a USSD session.
func (a *serviceAdapter) CancelUSSD(ctx context.Context, sessionID string) error {
	if a == nil || a.svc == nil {
		return errNoService
	}
	return a.svc.CancelUSSD(ctx, sessionID)
}

// Status returns the runtime status.
func (a *serviceAdapter) Status() Status {
	if a == nil || a.svc == nil {
		return Status{}
	}
	return Status{State: State{
		SessionState: "established",
		IMSState:     a.svc.RegState(),
		RegStatus:    regStatusOf(a.svc),
		DeviceID:     a.svc.DeviceID(),
		IMSReady:     a.svc.IsRegistered(),
	}}
}

// StatusSnapshot returns the runtime status snapshot.
func (a *serviceAdapter) StatusSnapshot() Status {
	return a.Status()
}

// Stop shuts the service down.
func (a *serviceAdapter) Stop() {
	if a == nil || a.svc == nil {
		return
	}
	a.svc.Stop()
}

// TriggerRegisterImmediate triggers an immediate re-registration.
func (a *serviceAdapter) TriggerRegisterImmediate() {
	if a == nil || a.svc == nil {
		return
	}
	a.svc.TriggerRegisterImmediate()
}

// regStatusOf maps the IMS registration state to a status code (1 = registered).
func regStatusOf(svc *imscore.Service) int {
	if svc == nil {
		return 0
	}
	if svc.IsRegistered() {
		return 1
	}
	return 0
}

// deliveryStoreAdapter adapts a messaging.DeliveryStore to the imscore
// DeliveryStore surface.
type deliveryStoreAdapter struct {
	store messaging.DeliveryStore
}

// newDeliveryStoreAdapter wraps a delivery store.
func newDeliveryStoreAdapter(store messaging.DeliveryStore) *deliveryStoreAdapter {
	return &deliveryStoreAdapter{store: store}
}

// CreateSMSDelivery creates a delivery record.
func (a *deliveryStoreAdapter) CreateSMSDelivery(messageID, imsi, deviceID, peer, content string, partsTotal int, at time.Time) error {
	if a == nil || a.store == nil {
		return errors.New("runtimehost: no delivery store")
	}
	return a.store.CreateSMSDelivery(messageID, imsi, deviceID, peer, content, partsTotal, at)
}

// UpsertSMSDeliveryPart upserts a delivery part.
func (a *deliveryStoreAdapter) UpsertSMSDeliveryPart(messageID string, partNo int, callID string, rpMR int, state string, sentAt time.Time) error {
	if a == nil || a.store == nil {
		return errors.New("runtimehost: no delivery store")
	}
	return a.store.UpsertSMSDeliveryPart(messageID, partNo, callID, rpMR, state, sentAt)
}

// MarkSMSDeliveryPartReport records a delivery report.
func (a *deliveryStoreAdapter) MarkSMSDeliveryPartReport(inReplyTo, callID, deviceID string, rpMR int, state string, sipCode int, rpCause int, errText string, at time.Time) (imscore.DeliveryPartMatch, error) {
	if a == nil || a.store == nil {
		return imscore.DeliveryPartMatch{}, errors.New("runtimehost: no delivery store")
	}
	m, err := a.store.MarkSMSDeliveryPartReport(inReplyTo, callID, deviceID, rpMR, state, sipCode, rpCause, errText, at)
	return imscore.DeliveryPartMatch{
		MessageID: m.MessageID,
		PartNo:    m.PartNo,
		State:     m.State,
		Matched:   m.Matched,
	}, err
}

// RecomputeSMSDelivery recomputes the delivery state.
func (a *deliveryStoreAdapter) RecomputeSMSDelivery(messageID string, at time.Time) error {
	if a == nil || a.store == nil {
		return errors.New("runtimehost: no delivery store")
	}
	return a.store.RecomputeSMSDelivery(messageID, at)
}

// UpdateSMSDeliveryState updates the delivery state.
func (a *deliveryStoreAdapter) UpdateSMSDeliveryState(messageID, state, lastError string, acks int, at time.Time) error {
	if a == nil || a.store == nil {
		return errors.New("runtimehost: no delivery store")
	}
	return a.store.UpdateSMSDeliveryState(messageID, state, lastError, acks, at)
}

// GetSMSDeliveryStatus returns the delivery status.
func (a *deliveryStoreAdapter) GetSMSDeliveryStatus(messageID string) (*imscore.DeliveryStatus, error) {
	if a == nil || a.store == nil {
		return nil, errors.New("runtimehost: no delivery store")
	}
	st, err := a.store.GetSMSDeliveryStatus(messageID)
	if err != nil {
		return nil, err
	}
	return deliveryStatusToInternal(st), nil
}

// deliveryStatusFromInternal converts an imscore delivery status to messaging.
func deliveryStatusFromInternal(st *imscore.DeliveryStatus) *messaging.DeliveryStatus {
	if st == nil {
		return nil
	}
	out := &messaging.DeliveryStatus{
		MessageID:  st.MessageID,
		IMSI:       st.IMSI,
		DeviceID:   st.DeviceID,
		Peer:       st.Peer,
		Content:    st.Content,
		PartsTotal: st.PartsTotal,
		Acks:       st.Acks,
		State:      st.State,
		LastError:  st.LastError,
	}
	for _, p := range st.Parts {
		out.Parts = append(out.Parts, messaging.DeliveryPartStatus{
			PartNo:  p.PartNo,
			CallID:  p.CallID,
			State:   p.State,
			SIPCode: p.SIPCode,
			RPCause: p.RPCause,
		})
	}
	return out
}

// deliveryStatusToInternal converts a messaging delivery status to imscore.
func deliveryStatusToInternal(st *messaging.DeliveryStatus) *imscore.DeliveryStatus {
	if st == nil {
		return nil
	}
	out := &imscore.DeliveryStatus{
		MessageID:  st.MessageID,
		IMSI:       st.IMSI,
		DeviceID:   st.DeviceID,
		Peer:       st.Peer,
		Content:    st.Content,
		PartsTotal: st.PartsTotal,
		Acks:       st.Acks,
		State:      st.State,
		LastError:  st.LastError,
	}
	for _, p := range st.Parts {
		out.Parts = append(out.Parts, imscore.DeliveryPartStatus{
			PartNo:  p.PartNo,
			CallID:  p.CallID,
			State:   p.State,
			SIPCode: p.SIPCode,
			RPCause: p.RPCause,
		})
	}
	return out
}

// eventDispatcherAdapter adapts an eventhost.Dispatcher to the EventDispatcher
// surface.
type eventDispatcherAdapter struct {
	dispatch func(Event)
}

// Dispatch delivers an event.
func (a *eventDispatcherAdapter) Dispatch(ev Event) {
	if a == nil || a.dispatch == nil {
		return
	}
	a.dispatch(ev)
}

// instanceObserver observes runtime events.
type instanceObserver struct {
	inst *Instance
}

// OnRuntimeEvent handles a runtime event.
func (o *instanceObserver) OnRuntimeEvent(ev Event) {
	if o == nil || o.inst == nil {
		return
	}
	o.inst.publish(ev)
}

// OnRuntimeHostEvent implements ObserverFunc as a method.
func (f ObserverFunc) OnRuntimeHostEvent(ctx context.Context, ev Event) {
	if f != nil {
		f(ctx, ev)
	}
}

// simAdapter adapts a SIM provider.
type simAdapter struct {
	provider SIMProvider
}

// runtimeSIMAdapter returns the underlying SIM provider.
func (a *simAdapter) runtimeSIMAdapter() SIMProvider {
	if a == nil {
		return nil
	}
	return a.provider
}

// AKAProvider returns the injected production SIM AKA provider.
func (a *simAdapter) AKAProvider() enginesim.AKAProvider {
	if a == nil {
		return nil
	}
	return a.provider
}

// SIMProvider computes AKA through the injected SIM implementation.
type SIMProvider = enginesim.AKAProvider

// apiErrorToInternal converts an API error to an internal error.
func apiErrorToInternal(err error) error {
	return err
}

// authPlanToInternal converts an auth plan to an internal value.
func authPlanToInternal(plan string) string {
	return plan
}

// defaultMainReconnectDelay returns the default reconnect delay for main mode.
func defaultMainReconnectDelay() time.Duration {
	return 5 * time.Second
}

// defaultReaderReconnectDelay returns the default reconnect delay for reader mode.
func defaultReaderReconnectDelay() time.Duration {
	return 10 * time.Second
}

// deliveryStoreErrorFromInternal converts an internal delivery error.
func deliveryStoreErrorFromInternal(err error) error {
	return err
}

// deliveryStoreErrorToInternal converts an external delivery error.
func deliveryStoreErrorToInternal(err error) error {
	return err
}

// moduleEventFromInternal converts an internal module event.
func moduleEventFromInternal(ev Event) Event {
	return ev
}

// preparedSessionFromInternal converts an internal prepared session.
func preparedSessionFromInternal(p *identity.PreparedSession) *identity.PreparedSession {
	return p
}

// preparedSessionPtrToInternal converts a prepared session pointer.
func preparedSessionPtrToInternal(p *identity.PreparedSession) *identity.PreparedSession {
	return p
}

// sessionConfigFromInternal converts an internal session config.
func sessionConfigFromInternal(c SessionConfig) SessionConfig {
	return c
}

// startInstance starts a runtime host instance.
func startInstance(ctx context.Context, req StartRequest) (*Instance, error) {
	return Start(ctx, req)
}

// startInstanceAsync starts a runtime host instance asynchronously.
func startInstanceAsync(ctx context.Context, req StartRequest) (<-chan *Instance, <-chan error) {
	instCh := make(chan *Instance, 1)
	errCh := make(chan error, 1)
	go func() {
		inst, err := Start(ctx, req)
		if err != nil {
			errCh <- err
			return
		}
		instCh <- inst
	}()
	return instCh, errCh
}

// startResultFromInternal converts an internal start result.
func startResultFromInternal(inst *Instance, err error) (*Instance, error) {
	return inst, err
}
