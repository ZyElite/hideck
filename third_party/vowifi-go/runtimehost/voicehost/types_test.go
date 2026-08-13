package voicehost

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestGatewayBuildsSimulatedCallCapturePath(t *testing.T) {
	directory := t.TempDir()
	gateway := NewGateway()
	gateway.SetPCAPDirectory(directory)

	path := gateway.simulatedCaptureBasePath("wwan/1", "")
	if filepath.Dir(path) != directory || !strings.HasPrefix(filepath.Base(path), "call_wwan_1_") {
		t.Fatalf("capture path=%q", path)
	}
	if explicit := gateway.simulatedCaptureBasePath("wwan1", "/tmp/requested"); explicit != "/tmp/requested" {
		t.Fatalf("explicit capture path=%q", explicit)
	}
}

// fakeAgent drives a simulated call without the voice engine.
type fakeAgent struct {
	dialed      string
	hungup      string
	pcapStarted string
	pcapStopped bool
	dtmfCallID  string
	dtmfDigit   string
}

func (f *fakeAgent) DialContext(_ context.Context, number string) (interface{}, error) {
	f.dialed = number
	return fakeCall{id: "call-1"}, nil
}
func (f *fakeAgent) HangupContext(_ context.Context, callID string) error {
	f.hungup = callID
	return nil
}
func (f *fakeAgent) Ready() bool  { return true }
func (f *fakeAgent) Start() error { return nil }
func (f *fakeAgent) Stop() error  { return nil }
func (f *fakeAgent) StartPCAP(output string) error {
	f.pcapStarted = output
	return nil
}
func (f *fakeAgent) StopPCAP() error {
	f.pcapStopped = true
	return nil
}
func (f *fakeAgent) SendDTMF(callID, digit string) error {
	f.dtmfCallID = callID
	f.dtmfDigit = digit
	return nil
}

type fakeCall struct{ id string }

func (c fakeCall) CallID() string { return c.id }

type fakeAudioTranscoder struct {
	input  string
	output string
	err    error
}

func (f *fakeAudioTranscoder) ToMP3(_ context.Context, input string) (string, error) {
	f.input = input
	return f.output, f.err
}

func TestGatewayFinalizesRecordingAsMP3(t *testing.T) {
	transcoder := &fakeAudioTranscoder{output: "/recordings/call.mp3"}
	gateway := NewGateway()
	gateway.SetAudioTranscoder(transcoder)
	result, err := gateway.finalizeSimulateCallAudio(context.Background(), &SimulateCallResult{
		Success: true, AudioPath: "/recordings/call.amr", AudioCodec: "AMR",
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if transcoder.input != "/recordings/call.amr" || result.AudioPath != transcoder.output || result.AudioCodec != "MP3" {
		t.Fatalf("transcoder=%+v result=%+v", transcoder, result)
	}
	if result.SourceAudioPath != "/recordings/call.amr" || result.SourceAudioCodec != "AMR" {
		t.Fatalf("source recording=%+v", result)
	}
}

func TestGatewayReportsMP3TranscodeFailure(t *testing.T) {
	transcoder := &fakeAudioTranscoder{err: errors.New("codec failed")}
	gateway := NewGateway()
	gateway.SetAudioTranscoder(transcoder)
	result, err := gateway.finalizeSimulateCallAudio(context.Background(), &SimulateCallResult{
		Success: true, AudioPath: "/recordings/call.amr", AudioCodec: "AMR",
	}, nil)
	if err == nil || result.Success || !strings.Contains(result.Reason, "codec failed") {
		t.Fatalf("result=%+v error=%v", result, err)
	}
}

type fakeMediaCall struct {
	fakeCall
	errors <-chan error
}

func (c fakeMediaCall) MediaErrors() <-chan error { return c.errors }

type fakeMediaErrorAgent struct {
	fakeAgent
	mediaErrors <-chan error
}

func (f *fakeMediaErrorAgent) DialContext(_ context.Context, number string) (interface{}, error) {
	f.dialed = number
	return fakeMediaCall{fakeCall: fakeCall{id: "call-media"}, errors: f.mediaErrors}, nil
}

type fakeIncomingAgent struct {
	fakeAgent
	handler  func(IncomingCall)
	answered AnswerRequest
	rejected RejectRequest
}

func (f *fakeIncomingAgent) SetIncomingCallHandler(handler func(IncomingCall)) { f.handler = handler }
func (f *fakeIncomingAgent) IncomingCalls() []IncomingCall {
	return []IncomingCall{{DeviceID: "dev-1", CallID: "incoming-1", OfferSDP: "v=0\r\n"}}
}
func (f *fakeIncomingAgent) AnswerIncomingCall(_ context.Context, callID, sdp string) (AnswerResult, error) {
	f.answered = AnswerRequest{CallID: callID, SDP: sdp}
	return AnswerResult{CallID: callID, State: "Connected"}, nil
}
func (f *fakeIncomingAgent) RejectIncomingCall(callID string, statusCode int) error {
	f.rejected = RejectRequest{CallID: callID, StatusCode: statusCode}
	return nil
}

func TestGatewaySimulateCall(t *testing.T) {
	g := NewGateway()
	agent := &fakeAgent{}
	g.SetAgent("dev-1", agent)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	res, err := g.SimulateCall(ctx, "dev-1", SimulateCallRequest{Callee: "+8613800000000", HoldSeconds: 1})
	if err != nil {
		t.Fatalf("SimulateCall: %v", err)
	}
	if !res.Success {
		t.Errorf("result = %+v", res)
	}
	if agent.dialed != "+8613800000000" {
		t.Errorf("dialed = %q", agent.dialed)
	}
	if agent.hungup != "call-1" {
		t.Errorf("hung up = %q, want call-1", agent.hungup)
	}
	if g.GetAgentCurrent("dev-1") != agent {
		t.Error("GetAgent mismatch")
	}
}

func TestGatewayNoAgent(t *testing.T) {
	g := NewGateway()
	if _, err := g.SimulateCall(context.Background(), "dev-1", SimulateCallRequest{}); err == nil {
		t.Error("should error without agent")
	}
}

func TestGatewayRoutesPCAPAndDTMFToRealAgent(t *testing.T) {
	gateway := NewGateway()
	agent := &fakeAgent{}
	if err := gateway.SetAgent("dev-1", agent); err != nil {
		t.Fatal(err)
	}
	gateway.SetPCAPDirectory(t.TempDir())
	if err := gateway.StartPCAPCurrent("dev-1"); err != nil {
		t.Fatal(err)
	}
	if agent.pcapStarted == "" {
		t.Fatal("PCAP directory was not forwarded")
	}
	if err := gateway.SendDTMF("dev-1", "call-1", "2"); err != nil {
		t.Fatal(err)
	}
	if agent.dtmfCallID != "call-1" || agent.dtmfDigit != "2" {
		t.Fatalf("DTMF forwarding = %q/%q", agent.dtmfCallID, agent.dtmfDigit)
	}
	if err := gateway.StopPCAP("dev-1"); err != nil {
		t.Fatal(err)
	}
	if !agent.pcapStopped {
		t.Fatal("StopPCAP was not forwarded")
	}
}

func TestGatewaySimulateCallReturnsMediaFailure(t *testing.T) {
	mediaErrors := make(chan error, 1)
	mediaErrors <- errors.New("RTP write failed")
	agent := &fakeMediaErrorAgent{mediaErrors: mediaErrors}
	gateway := NewGateway()
	if err := gateway.SetAgent("dev-1", agent); err != nil {
		t.Fatal(err)
	}
	result, err := gateway.SimulateCall(context.Background(), "dev-1", SimulateCallRequest{
		Callee: "+8613800000000", HoldSeconds: 1,
	})
	if err == nil || result.Success || result.Reason != "RTP write failed" {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	if agent.hungup != "call-media" {
		t.Fatalf("hung up = %q, want call-media", agent.hungup)
	}
}

func TestGatewayExposesIncomingCallLifecycle(t *testing.T) {
	gateway := NewGateway()
	delivered := make(chan IncomingCall, 1)
	gateway.SetIncomingCallHandler(func(call IncomingCall) { delivered <- call })
	agent := &fakeIncomingAgent{}
	if err := gateway.SetAgent("dev-1", agent); err != nil {
		t.Fatal(err)
	}
	agent.handler(IncomingCall{DeviceID: "dev-1", CallID: "incoming-1"})
	assertIncomingCall(t, delivered, "incoming-1")
	calls, err := gateway.IncomingCalls("dev-1")
	if err != nil || len(calls) != 1 || calls[0].CallID != "incoming-1" {
		t.Fatalf("IncomingCalls calls=%+v err=%v", calls, err)
	}
	answer, err := gateway.AnswerIncomingCall(context.Background(), AnswerRequest{
		DeviceID: "dev-1", CallID: "incoming-1", SDP: "v=0\r\n",
	})
	if err != nil || answer.State != "Connected" || agent.answered.CallID != "incoming-1" {
		t.Fatalf("answer=%+v recorded=%+v err=%v", answer, agent.answered, err)
	}
	if err := gateway.RejectIncomingCall(RejectRequest{DeviceID: "dev-1", CallID: "incoming-2"}); err != nil {
		t.Fatal(err)
	}
	if agent.rejected.StatusCode != 486 {
		t.Fatalf("reject = %+v", agent.rejected)
	}
}
