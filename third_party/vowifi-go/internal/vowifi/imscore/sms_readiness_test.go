package imscore

import "testing"

func TestEvaluateSMSReadinessRequiresEveryPrerequisite(t *testing.T) {
	tests := []struct {
		name       string
		registered bool
		receiver   bool
		smsc       string
		ready      bool
		reason     string
	}{
		{name: "registration", receiver: true, smsc: "+123", reason: smsReadyReasonNotRegistered},
		{name: "receiver", registered: true, smsc: "+123", reason: smsReadyReasonReceiverNotReady},
		{name: "smsc", registered: true, receiver: true, reason: smsReadyReasonSMSCNotConfigured},
		{name: "ready", registered: true, receiver: true, smsc: "+123", ready: true, reason: smsReadyReasonReady},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := evaluateSMSReadiness(test.registered, test.receiver, test.smsc)
			if got.Ready != test.ready || got.Reason != test.reason {
				t.Fatalf("readiness = %+v", got)
			}
		})
	}
}

func TestSMSReadinessObserverReceivesCurrentAndChangedState(t *testing.T) {
	service, err := New(&IMSConfig{SMSC: "+123"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	var states []SMSReadiness
	service.SetOnSMSReadinessChanged(func(state SMSReadiness) {
		states = append(states, state)
	})
	service.setSMSReceiverReady(true)
	if len(states) != 2 {
		t.Fatalf("observer states = %d, want 2", len(states))
	}
	if states[0].ReceiverReady || !states[1].ReceiverReady {
		t.Fatalf("observer states = %+v", states)
	}
}
