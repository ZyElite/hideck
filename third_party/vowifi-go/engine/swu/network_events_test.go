package swu

import (
	"testing"
	"time"

	"github.com/iniwex5/vowifi-go/engine/ipsec"
)

func TestNetworkEventMonitorAppliesPMTUAndFailsRepeatedPreKeyOutage(t *testing.T) {
	session := NewSession(&Config{})
	transport := newTestIKETransport()
	session.setTransport(transport)
	session.startNetEventMonitor()
	session.startNetEventMonitor()
	transport.netEvents <- ipsec.NetEvent{Type: ipsec.EventPathMTU, PMTU: 900}
	waitForSWuCondition(t, func() bool {
		session.mu.RLock()
		defer session.mu.RUnlock()
		return session.ikeFragmentMTU == 900
	})
	for range preKeyUnreachableThreshold {
		transport.netEvents <- ipsec.NetEvent{Type: ipsec.EventNetworkDown, Reason: "host unreachable"}
	}
	waitForSWuCondition(t, func() bool { return session.TerminalError() != nil })
	session.netEventWG.Wait()
	if session.ctx.Err() == nil {
		t.Fatal("repeated pre-key network outage did not cancel the session")
	}
}

func TestNetworkDownEventRunsDPDOnEstablishedControlPlane(t *testing.T) {
	session, transport := newEstablishedControlSession(t)
	defer session.Shutdown()
	session.startNetEventMonitor()
	go answerEmptyInformational(session, transport)
	transport.netEvents <- ipsec.NetEvent{Type: ipsec.EventNetworkDown, Reason: "route changed"}
	waitForSWuCondition(t, func() bool {
		session.mu.RLock()
		defer session.mu.RUnlock()
		return !session.lastDPDAt.IsZero()
	})
}

func waitForSWuCondition(t *testing.T, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for !condition() {
		if time.Now().After(deadline) {
			t.Fatal("timed out waiting for SWu condition")
		}
		time.Sleep(time.Millisecond)
	}
}
