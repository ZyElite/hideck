package imscore

import "testing"

func TestEvaluateSMSReadinessRequiresEveryPrerequisite(t *testing.T) {
	tests := []struct {
		name       string
		registered bool
		profile    bool
		transport  bool
		receiver   bool
		smsc       string
		ready      bool
		reason     string
	}{
		{name: "registration", profile: true, transport: true, receiver: true, smsc: "+123", reason: smsReadyReasonNotRegistered},
		{name: "profile", registered: true, transport: true, receiver: true, smsc: "+123", reason: smsReadyReasonProfileNotReady},
		{name: "transport", registered: true, profile: true, receiver: true, smsc: "+123", reason: smsReadyReasonTransportNotReady},
		{name: "receiver", registered: true, profile: true, transport: true, smsc: "+123", reason: smsReadyReasonReceiverNotReady},
		{name: "smsc", registered: true, profile: true, transport: true, receiver: true, reason: smsReadyReasonSMSCNotConfigured},
		{name: "ready", registered: true, profile: true, transport: true, receiver: true, smsc: "+123", ready: true, reason: smsReadyReasonReady},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := evaluateSMSReadiness(test.registered, test.profile, test.transport, test.receiver, test.smsc)
			if got.Ready != test.ready || got.Reason != test.reason {
				t.Fatalf("readiness = %+v", got)
			}
		})
	}
}

func TestSMSReadinessRequiresNegotiatedIdentityAndContact(t *testing.T) {
	service, err := New(&IMSConfig{SMSC: "+123"})
	if err != nil {
		t.Fatal(err)
	}
	service.mu.Lock()
	service.regState = regRegistered
	service.smsReceiverReady = true
	service.regSession = &registerSession{contactUser: "binding"}
	service.mu.Unlock()
	if got := service.SMSReadiness(); got.Ready || got.Reason != smsReadyReasonProfileNotReady {
		t.Fatalf("readiness without associated identity = %+v", got)
	}
	service.mu.Lock()
	service.regSession.publicID = "sip:+15551234567@ims.example"
	service.externalTransport = true
	service.mu.Unlock()
	if got := service.SMSReadiness(); !got.Ready || !got.ProfileReady {
		t.Fatalf("readiness with registered profile = %+v", got)
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
