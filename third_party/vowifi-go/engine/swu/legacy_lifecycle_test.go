package swu

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/iniwex5/vowifi-go/engine/ikev2"
)

func TestLegacyReauthCallbackGetsOverlapGrace(t *testing.T) {
	callback := make(chan struct{}, 1)
	session := NewSession(&Config{ReauthSeconds: time.Millisecond})
	session.OnReauthNeeded = func() { callback <- struct{}{} }
	session.reauthOverlapGrace = 25 * time.Millisecond
	session.setState(stateEstablished)
	session.startIKEReauthTimer()
	select {
	case <-callback:
	case <-time.After(time.Second):
		t.Fatal("reauth timer did not invoke OnReauthNeeded")
	}
	select {
	case <-session.done:
		t.Fatal("session closed before the overlap grace elapsed")
	default:
	}
	waitCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := session.WaitDoneContext(waitCtx); err != nil {
		t.Fatalf("session did not close after overlap grace: %v", err)
	}
	if !errors.Is(session.TerminalError(), ErrFreshRuntimeRequired) {
		t.Fatalf("terminal error = %v", session.TerminalError())
	}
	session.Shutdown()
}

func TestRuntimeRedirectInvokesLegacyCallbacksAfterResponse(t *testing.T) {
	session, transport := newEstablishedControlSession(t)
	defer stopControlTestSession(session)
	session.controlMu.Lock()
	session.controlRunning = false
	session.controlMu.Unlock()
	redirected := make(chan string, 1)
	down := make(chan struct{}, 1)
	session.OnRedirect = func(address string) { redirected <- address }
	session.OnSessionDown = func() { down <- struct{}{} }
	request := &ikev2.IKEPacket{
		Header: newIKEHeader(session.spiI, session.spiR, ikev2.INFORMATIONAL, 0, 31),
		Payloads: []ikev2.Payload{&ikev2.EncryptedPayloadNotify{
			NotifyType: ikev2.REDIRECT,
			NotifyData: []byte{3, 'e', 'p', 'd', 'g', '.', 'x'},
		}},
	}
	raw, err := session.encryptAndWrap(request)
	if err != nil {
		t.Fatalf("encrypt REDIRECT: %v", err)
	}
	decoded, err := ikev2.DecodePacket(raw)
	if err != nil {
		t.Fatalf("decode REDIRECT: %v", err)
	}
	if err := session.handlePeerInformational(decoded); err != nil {
		t.Fatalf("handle REDIRECT: %v", err)
	}
	_ = receiveFragmentPacket(t, transport.sentIKE)
	select {
	case address := <-redirected:
		if address != "epdg.x" {
			t.Fatalf("redirect address = %q", address)
		}
	case <-time.After(time.Second):
		t.Fatal("runtime REDIRECT callback was not invoked")
	}
	select {
	case <-down:
	case <-time.After(time.Second):
		t.Fatal("runtime REDIRECT did not signal session down")
	}
}

func TestEstablishedFailureNotifiesSessionDownExactlyOnce(t *testing.T) {
	session := NewSession(&Config{})
	down := make(chan struct{}, 2)
	session.OnSessionDown = func() { down <- struct{}{} }
	session.setState(stateEstablished)
	session.failEstablishedControl(errors.New("DPD failed"))
	session.failEstablishedControl(errors.New("duplicate failure"))
	select {
	case <-down:
	case <-time.After(time.Second):
		t.Fatal("established failure did not notify OnSessionDown")
	}
	select {
	case <-down:
		t.Fatal("established failure notified OnSessionDown more than once")
	case <-time.After(20 * time.Millisecond):
	}
	waitCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := session.WaitDoneContext(waitCtx); err != nil {
		t.Fatalf("failed session cleanup: %v", err)
	}
}
