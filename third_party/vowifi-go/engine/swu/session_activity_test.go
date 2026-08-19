package swu

import (
	"errors"
	"testing"
	"time"
)

func TestNATKeepaliveDecisionRestoresTrafficAwareness(t *testing.T) {
	interval := 20 * time.Second
	tests := []struct {
		name               string
		idle               time.Duration
		wantDelay          time.Duration
		wantKeepalive, dpd bool
	}{
		{name: "recent traffic", idle: 5 * time.Second, wantDelay: 15 * time.Second},
		{name: "keepalive due", idle: interval, wantDelay: interval, wantKeepalive: true},
		{name: "dpd margin exceeded", idle: interval + natKeepaliveDPDMargin + time.Nanosecond, wantDelay: interval, dpd: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			delay, keepalive, dpd := natKeepaliveDecision(interval, test.idle)
			if delay != test.wantDelay || keepalive != test.wantKeepalive || dpd != test.dpd {
				t.Fatalf("decision = %v/%v/%v, want %v/%v/%v", delay, keepalive, dpd,
					test.wantDelay, test.wantKeepalive, test.dpd)
			}
		})
	}
}

func TestNATKeepaliveStartsForSOCKS5WithoutNAT(t *testing.T) {
	session := NewSession(&Config{ProxyAddr: "127.0.0.1:1080"})
	session.startNATKeepalive(time.Hour)
	session.timersMu.Lock()
	armed := session.natKeepalive != nil
	session.timersMu.Unlock()
	if !armed || session.natDetected {
		t.Fatalf("socks5 keepalive armed=%v natDetected=%v", armed, session.natDetected)
	}
	session.Shutdown()
}

func TestRecoveredTimerIntervalCallShapes(t *testing.T) {
	session := NewSession(&Config{})
	session.natDetected = true
	session.startIKEReauthTimer(time.Hour)
	session.startNATKeepalive(time.Hour)
	session.StartDPD(time.Hour)
	session.timersMu.Lock()
	armed := session.ikeReauthTimer != nil && session.natKeepalive != nil && session.dpdTimer != nil
	session.timersMu.Unlock()
	if !armed {
		t.Fatal("legacy interval-taking timer calls did not arm every timer")
	}
	session.Shutdown()
}

func TestSuccessfulTransportWritesUpdateOutboundActivity(t *testing.T) {
	session := NewSession(&Config{})
	transport := newTestIKETransport()
	if err := session.sendIKEPacketSet(transport, [][]byte{{1}}); err != nil {
		t.Fatalf("sendIKEPacketSet: %v", err)
	}
	assertOutboundActivity(t, session)

	session.activityMu.Lock()
	session.lastOutboundTime = time.Time{}
	session.activityMu.Unlock()
	transport.sendIKEErr = errors.New("send rejected")
	if err := session.sendIKEPacketSet(transport, [][]byte{{2}}); err == nil {
		t.Fatal("sendIKEPacketSet hid transport failure")
	}
	session.activityMu.RLock()
	last := session.lastOutboundTime
	session.activityMu.RUnlock()
	if !last.IsZero() {
		t.Fatal("failed transport write was recorded as outbound activity")
	}
}

func assertOutboundActivity(t *testing.T, session *Session) {
	t.Helper()
	session.activityMu.RLock()
	last := session.lastOutboundTime
	session.activityMu.RUnlock()
	if last.IsZero() {
		t.Fatal("successful transport write did not record outbound activity")
	}
}
