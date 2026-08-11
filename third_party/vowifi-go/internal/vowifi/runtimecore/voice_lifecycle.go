package runtimecore

import (
	"context"
	"strings"
	"sync"

	"github.com/iniwex5/vowifi-go/internal/vowifi/imsendpoint"
	"github.com/iniwex5/vowifi-go/internal/vowifi/profile"
	"go.uber.org/zap"
)

type voiceLifecycleBinding struct {
	deviceID     string
	voice        VoiceLifecycle
	mu           sync.Mutex
	effectMu     sync.Mutex
	lastEndpoint imsendpoint.Endpoint
	attached     bool
	stopped      bool
}

func (binding *voiceLifecycleBinding) AttachIfReady(endpoint imsendpoint.Endpoint) {
	if binding == nil || binding.voice == nil || endpoint == nil {
		return
	}
	binding.mu.Lock()
	if binding.stopped || binding.lastEndpoint == endpoint && binding.attached {
		binding.mu.Unlock()
		return
	}
	binding.lastEndpoint = endpoint
	binding.attached = false
	deviceID := binding.deviceID
	voice := binding.voice
	binding.mu.Unlock()

	binding.effectMu.Lock()
	defer binding.effectMu.Unlock()
	binding.mu.Lock()
	if binding.stopped || binding.lastEndpoint != endpoint || binding.attached {
		binding.mu.Unlock()
		return
	}
	binding.mu.Unlock()
	err := voice.AttachDevice(deviceID, endpoint)
	binding.mu.Lock()
	stale := binding.stopped || binding.lastEndpoint != endpoint
	if !stale && err == nil {
		binding.attached = true
	}
	binding.mu.Unlock()
	if err != nil {
		zap.S().Warnw("failed to attach voice endpoint", "device", deviceID, "error", err)
		return
	}
	if stale {
		voice.DetachDevice(deviceID)
	}
}

func (binding *voiceLifecycleBinding) Detach() {
	if binding == nil || binding.voice == nil {
		return
	}
	binding.mu.Lock()
	if !binding.attached && binding.lastEndpoint == nil {
		binding.mu.Unlock()
		return
	}
	attached := binding.attached
	binding.attached = false
	binding.lastEndpoint = nil
	deviceID := binding.deviceID
	voice := binding.voice
	binding.mu.Unlock()
	binding.effectMu.Lock()
	if attached {
		voice.DetachDevice(deviceID)
	}
	binding.effectMu.Unlock()
}

func (binding *voiceLifecycleBinding) Stop() {
	if binding == nil || binding.voice == nil {
		return
	}
	binding.mu.Lock()
	if binding.stopped {
		binding.mu.Unlock()
		return
	}
	binding.stopped = true
	attached := binding.attached
	binding.attached = false
	binding.lastEndpoint = nil
	deviceID := binding.deviceID
	voice := binding.voice
	binding.mu.Unlock()
	binding.effectMu.Lock()
	defer binding.effectMu.Unlock()
	if attached {
		voice.DetachDevice(deviceID)
	}
}

type imsRegisteredNotifier struct {
	ctx      context.Context
	events   RuntimeObserver
	hooks    RuntimeHostHooks
	voice    *voiceLifecycleBinding
	device   string
	traceID  string
	identity profile.IMSIdentityResult
	mu       sync.Mutex
	session  *SessionResult
	pending  bool
}

func newIMSRegisteredNotifier(
	ctx context.Context,
	req *RuntimeStartRequest,
	identity profile.IMSIdentityResult,
) *imsRegisteredNotifier {
	return &imsRegisteredNotifier{
		ctx: ctx, events: req.Observer, hooks: req.Hooks, voice: req.voiceBinding,
		device: strings.TrimSpace(req.DeviceID), traceID: strings.TrimSpace(req.TraceID), identity: identity,
	}
}

func (notifier *imsRegisteredNotifier) SetSession(session *SessionResult) {
	if notifier == nil {
		return
	}
	notifier.mu.Lock()
	notifier.session = session
	pending := notifier.pending
	notifier.pending = false
	notifier.mu.Unlock()
	if pending && session != nil && session.IMSService != nil && notifier.voice != nil {
		notifier.voice.AttachIfReady(session.IMSService)
	}
	if pending && session != nil {
		notifier.emitRegistered(session)
	}
}

func (notifier *imsRegisteredNotifier) OnIMSRegistered() {
	if notifier == nil {
		return
	}
	notifier.mu.Lock()
	session := notifier.session
	if session == nil {
		notifier.pending = true
		notifier.mu.Unlock()
		return
	}
	notifier.mu.Unlock()
	if session != nil && session.IMSService != nil && notifier.voice != nil {
		notifier.voice.AttachIfReady(session.IMSService)
	}
	notifier.emitRegistered(session)
}

func (notifier *imsRegisteredNotifier) emitRegistered(session *SessionResult) {
	event := RuntimeEvent[*SessionResult]{
		Kind: "ims_registered", Handle: session, DeviceID: notifier.device,
		TraceID: notifier.traceID, Identity: notifier.identity,
		Snapshot: snapshotFromSessionResult(session),
	}
	if session != nil {
		event.Service = session.IMSService
	}
	if notifier.events != nil {
		notifier.events.OnRuntimeEvent(notifier.ctx, event)
	}
	if notifier.hooks.Events != nil {
		notifier.hooks.Events.OnRuntimeEvent(notifier.ctx, event)
	}
	if notifier.hooks.OnIMSRegistered != nil {
		notifier.hooks.OnIMSRegistered(notifier.ctx)
	}
}
