// Package voicehost exposes the runtime voice gateway boundary.
package voicehost

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/iniwex5/vowifi-go/internal/vowifi/voice"
	"github.com/iniwex5/vowifi-go/runtimehost/eventhost"
)

const (
	DefaultSimulateCallHoldSeconds = 10
	MaxSimulateCallHoldSeconds     = 60
)

// SimulateCallRequest is the recovered v1.5.5 timed-call request.
type SimulateCallRequest struct {
	Callee          string `json:"callee"`
	HoldSeconds     int    `json:"hold_seconds,omitempty"`
	OnConnected     func() `json:"-" binding:"-"`
	CaptureBasePath string `json:"-" binding:"-"`
}

// SimulateCallResult retains the recovered prefix; Message is additive.
type SimulateCallResult struct {
	Success          bool   `json:"success"`
	DurationMs       int64  `json:"duration_ms"`
	Reason           string `json:"reason"`
	Message          string `json:"message,omitempty"`
	PCAPPath         string `json:"pcap_path,omitempty"`
	AudioPath        string `json:"audio_path,omitempty"`
	AudioCodec       string `json:"audio_codec,omitempty"`
	SourceAudioPath  string `json:"source_audio_path,omitempty"`
	SourceAudioCodec string `json:"source_audio_codec,omitempty"`
}

type Notifier interface{}

// AudioTranscoder is injected by the host because codec libraries are infrastructure.
type AudioTranscoder interface {
	ToMP3(context.Context, string) (string, error)
}

// Profile is retained for callers of the additive host API.
type Profile struct {
	DeviceID string
	IMSI     string
	IMPI     string
	Domain   string
}

// Gateway retains the recovered inner pointer as its exact field prefix.
type Gateway struct {
	inner *voice.Gateway

	mu                   sync.RWMutex
	agents               map[string]voiceAgent
	currentClient        ClientAdapterCurrent
	innerDevices         map[string]struct{}
	pcapDirectory        string
	audioTranscoder      AudioTranscoder
	started              bool
	nextSubscriptionID   uint64
	incomingLegacy       *incomingSubscription
	incomingSubscribers  map[uint64]*incomingSubscription
	callEventSubscribers map[uint64]*callEventSubscription
	incomingSeen         map[string]struct{}
	eventDispatcher      eventhost.Dispatcher
}

// CallEvent is the stable host projection of voice lifecycle events.
type CallEvent struct {
	Type           string    `json:"type"`
	DeviceID       string    `json:"device_id"`
	CallID         string    `json:"call_id"`
	Caller         string    `json:"caller,omitempty"`
	Callee         string    `json:"callee,omitempty"`
	Reason         string    `json:"reason,omitempty"`
	Direction      string    `json:"direction,omitempty"`
	State          string    `json:"state,omitempty"`
	PCAPPath       string    `json:"pcap_path,omitempty"`
	AudioPath      string    `json:"audio_path,omitempty"`
	AudioCodec     string    `json:"audio_codec,omitempty"`
	RecordingError string    `json:"recording_error,omitempty"`
	Time           time.Time `json:"time"`
}

// NewGateway is additive; the original Gateway was constructed by runtimehost.
func NewGateway() *Gateway {
	return &Gateway{
		inner:                voice.NewGateway(nil),
		agents:               make(map[string]voiceAgent),
		innerDevices:         make(map[string]struct{}),
		incomingSubscribers:  make(map[uint64]*incomingSubscription),
		callEventSubscribers: make(map[uint64]*callEventSubscription),
		incomingSeen:         make(map[string]struct{}),
	}
}

// Start delegates the recovered lifecycle to the real voice gateway.
func (g *Gateway) Start(ctx context.Context) error {
	if g == nil {
		return nil
	}
	if g.inner != nil {
		if err := g.inner.Start(ctx); err != nil {
			return err
		}
	}
	agents, alreadyStarted := g.markStartedAndSnapshotAgents()
	if alreadyStarted {
		return nil
	}
	for index, agent := range agents {
		if err := agent.Start(); err != nil {
			stopVoiceAgents(agents[:index])
			g.markStopped()
			if g.inner != nil {
				_ = g.inner.Stop()
			}
			return err
		}
	}
	return nil
}

func (g *Gateway) markStartedAndSnapshotAgents() ([]voiceAgent, bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.started {
		return nil, true
	}
	g.started = true
	agents := make([]voiceAgent, 0, len(g.agents))
	for _, agent := range g.agents {
		agents = append(agents, agent)
	}
	return agents, false
}

// Stop releases the real registry and every additive compatibility agent.
func (g *Gateway) Stop() error {
	if g == nil {
		return nil
	}
	agents := g.markStopped()
	var err error
	if g.inner != nil {
		err = g.inner.Stop()
	}
	for _, agent := range agents {
		err = errors.Join(err, agent.Stop())
	}
	return err
}

func (g *Gateway) markStopped() []voiceAgent {
	g.mu.Lock()
	defer g.mu.Unlock()
	if !g.started {
		return nil
	}
	g.started = false
	agents := make([]voiceAgent, 0, len(g.agents))
	for _, agent := range g.agents {
		agents = append(agents, agent)
	}
	return agents
}

func stopVoiceAgents(agents []voiceAgent) {
	for _, agent := range agents {
		_ = agent.Stop()
	}
}

// SetNotifier restores the original incoming-call notifier contract.
func (g *Gateway) SetNotifier(notifier Notifier) {
	if g == nil || g.inner == nil {
		return
	}
	if notifier == nil {
		g.inner.SetNotifier(nil)
		return
	}
	g.inner.SetNotifier(notifier.(voice.CallNotifier))
}

// GetAgent returns the recovered empty-interface projection of the real Agent.
func (g *Gateway) GetAgent(deviceID string) interface{} {
	agent := g.internalAgent(deviceID)
	if agent == nil {
		return nil
	}
	return agent
}

func (g *Gateway) internalAgent(deviceID string) *voice.Agent {
	if g == nil || g.inner == nil {
		return nil
	}
	return g.inner.GetAgent(strings.TrimSpace(deviceID))
}

// DeviceStatus returns the recovered device envelope.
func (g *Gateway) DeviceStatus(deviceID string) map[string]interface{} {
	if g == nil || g.inner == nil {
		return map[string]interface{}{}
	}
	return g.inner.DeviceStatus(deviceID)
}

// SimulateCall runs the real recovered IMS/media workflow.
func (g *Gateway) SimulateCall(
	ctx context.Context,
	deviceID string,
	request SimulateCallRequest,
) (*SimulateCallResult, error) {
	request.CaptureBasePath = g.simulatedCaptureBasePath(deviceID, request.CaptureBasePath)
	if agent := g.internalAgent(deviceID); agent != nil {
		result, err := g.inner.SimulateCall(ctx, deviceID, toVoiceSimulateRequest(request))
		return g.finalizeSimulateCallAudio(ctx, fromVoiceSimulateResult(result), err)
	}
	if g != nil && g.currentVoiceAgent(deviceID) != nil {
		result, err := g.simulateCallWithCurrentAgent(ctx, deviceID, request)
		return g.finalizeSimulateCallAudio(ctx, result, err)
	}
	if g == nil || g.inner == nil {
		return nil, nil
	}
	result, err := g.inner.SimulateCall(ctx, deviceID, toVoiceSimulateRequest(request))
	return g.finalizeSimulateCallAudio(ctx, fromVoiceSimulateResult(result), err)
}

func (g *Gateway) finalizeSimulateCallAudio(
	ctx context.Context,
	result *SimulateCallResult,
	callErr error,
) (*SimulateCallResult, error) {
	if callErr != nil || result == nil || strings.TrimSpace(result.AudioPath) == "" {
		return result, callErr
	}
	g.mu.RLock()
	transcoder := g.audioTranscoder
	g.mu.RUnlock()
	if transcoder == nil {
		err := errors.New("voicehost: MP3 audio transcoder is not configured")
		result.Success, result.Reason = false, err.Error()
		return result, err
	}
	outputPath, err := transcoder.ToMP3(ctx, result.AudioPath)
	if err != nil {
		err = fmt.Errorf("voicehost: transcode call recording to MP3: %w", err)
		result.Success, result.Reason = false, err.Error()
		return result, err
	}
	result.SourceAudioPath, result.SourceAudioCodec = result.AudioPath, result.AudioCodec
	result.AudioPath, result.AudioCodec = outputPath, "MP3"
	return result, nil
}

func toVoiceSimulateRequest(request SimulateCallRequest) voice.SimulateCallRequest {
	return voice.SimulateCallRequest{
		Callee: request.Callee, HoldSeconds: request.HoldSeconds,
		OnConnected: request.OnConnected, CaptureBasePath: request.CaptureBasePath,
	}
}

func fromVoiceSimulateResult(result *voice.SimulateCallResult) *SimulateCallResult {
	if result == nil {
		return nil
	}
	converted := &SimulateCallResult{
		Success: result.Success, DurationMs: result.DurationMs, Reason: result.Reason,
		PCAPPath: result.PCAPPath, AudioPath: result.AudioPath, AudioCodec: result.AudioCodec,
	}
	if converted.Success {
		converted.Message = "call completed"
	}
	return converted
}
