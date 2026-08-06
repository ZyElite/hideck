package runtimehost

import (
	"context"
	"errors"
	"testing"
)

type stubService struct {
	sentSMS bool
	ussd    string
}

func (s *stubService) SendSMSWithOptions(ctx context.Context, to, text string, opts SendOptions) (SendOutcome, error) {
	s.sentSMS = true
	return SendOutcome{Ref: "ref-1"}, nil
}
func (s *stubService) SendSMSWithResult(ctx context.Context, to, text string) (SendOutcome, error) {
	return s.SendSMSWithOptions(ctx, to, text, SendOptions{})
}
func (s *stubService) GetSMSDeliveryStatus(ctx context.Context, ref string) (*DeliveryStatus, error) {
	return &DeliveryStatus{State: "delivered"}, nil
}
func (s *stubService) SendUSSD(ctx context.Context, code string) (*USSDResult, error) {
	s.ussd = code
	return &USSDResult{Code: "0", Message: "ok"}, nil
}
func (s *stubService) ContinueUSSD(ctx context.Context, sessionID, input string) (*USSDResult, error) {
	return &USSDResult{}, nil
}
func (s *stubService) CancelUSSD(ctx context.Context, sessionID string) error { return nil }
func (s *stubService) Status() Status                                        { return Status{} }
func (s *stubService) StatusSnapshot() Status                                { return Status{} }
func (s *stubService) Stop()                                                  {}
func (s *stubService) TriggerRegisterImmediate()                              {}

func TestInstanceStateAndService(t *testing.T) {
	i := &Instance{}
	if i.State() != (State{}) {
		t.Error("initial state should be zero")
	}
	if i.Service() != nil {
		t.Error("initial service should be nil")
	}
	svc := &stubService{}
	i.setService(svc)
	if i.Service() != svc {
		t.Error("setService did not install the service")
	}
	i.setState(State{SessionState: "established"})
	if i.State().SessionState != "established" {
		t.Errorf("state = %+v", i.State())
	}
}

func TestInstanceObservers(t *testing.T) {
	i := &Instance{}
	var got []Event
	i.AddObserver(func(_ context.Context, ev Event) { got = append(got, ev) })
	i.setState(State{SessionState: "connecting"})
	if len(got) != 1 || got[0].Detail != "connecting" {
		t.Errorf("observer events = %+v", got)
	}
}

func TestInstanceSMSDelegation(t *testing.T) {
	ctx := context.Background()
	i := &Instance{}
	if _, err := i.SendSMSWithResult(ctx, "+8613800000000", "hi"); !errors.Is(err, errNoService) {
		t.Errorf("no-service err = %v", err)
	}
	svc := &stubService{}
	i.setService(svc)
	out, err := i.SendSMSWithResult(ctx, "+8613800000000", "hi")
	if err != nil || out.Ref != "ref-1" || !svc.sentSMS {
		t.Errorf("SendSMSWithResult = %+v err %v", out, err)
	}
	ds, err := i.GetSMSDeliveryStatus(ctx, "ref-1")
	if err != nil || ds.State != "delivered" {
		t.Errorf("delivery status = %+v err %v", ds, err)
	}
	res, err := i.SendUSSD(ctx, "*100#")
	if err != nil || svc.ussd != "*100#" || res.Code != "0" {
		t.Errorf("USSD = %+v err %v", res, err)
	}
}

func TestInstanceStop(t *testing.T) {
	i := &Instance{}
	svc := &stubService{}
	i.setService(svc)
	if err := i.Stop(context.Background()); err != nil {
		t.Errorf("Stop = %v", err)
	}
	if err := i.Stop(context.Background()); err != nil {
		t.Errorf("Stop (idempotent) = %v", err)
	}
}

func TestNewTraceID(t *testing.T) {
	a, b := NewTraceID(), NewTraceID()
	if a == "" || a == b {
		t.Errorf("trace ids = %q %q", a, b)
	}
}
