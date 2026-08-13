package phone

import (
	"errors"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/pion/webrtc/v4"
)

const silenceLifecycleTimeout = time.Second

func TestSilentRTPFailureReportsFailedOnceAndStops(t *testing.T) {
	states := make(chan webrtc.PeerConnectionState, 2)
	session := newSilentLifecycleSession(t, failingSilentCodec{}, func(_ string, state webrtc.PeerConnectionState) {
		states <- state
	})
	session.startSilentRTP()
	expectSingleFailedState(t, states)
	if err := session.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestSilentRTPWriteFailureReportsFailedOnceAndStops(t *testing.T) {
	states := make(chan webrtc.PeerConnectionState, 2)
	session := newSilentLifecycleSession(t, encodedSilentCodec{}, func(_ string, state webrtc.PeerConnectionState) {
		states <- state
	})
	if err := session.rtpConn.Close(); err != nil {
		t.Fatal(err)
	}
	session.startSilentRTP()
	expectSingleFailedState(t, states)
	_ = session.Close()
}

func expectSingleFailedState(t *testing.T, states <-chan webrtc.PeerConnectionState) {
	t.Helper()
	select {
	case state := <-states:
		if state != webrtc.PeerConnectionStateFailed {
			t.Fatalf("silent RTP state = %s, want failed", state)
		}
	case <-time.After(silenceLifecycleTimeout):
		t.Fatal("silent RTP failure was not reported")
	}
	select {
	case state := <-states:
		t.Fatalf("silent RTP failure reported more than once: %s", state)
	case <-time.After(3 * jitterTick):
	}
}

func TestMediaSessionCloseWaitsForSilentRTPWorker(t *testing.T) {
	codec := &blockingSilentCodec{entered: make(chan struct{}), release: make(chan struct{})}
	session := newSilentLifecycleSession(t, codec, nil)
	session.startSilentRTP()
	select {
	case <-codec.entered:
	case <-time.After(silenceLifecycleTimeout):
		t.Fatal("silent RTP worker did not enter codec")
	}
	closed := make(chan error, 1)
	go func() { closed <- session.Close() }()
	select {
	case err := <-closed:
		t.Fatalf("Close returned before silent RTP worker exited: %v", err)
	case <-time.After(3 * jitterTick):
	}
	close(codec.release)
	select {
	case err := <-closed:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(silenceLifecycleTimeout):
		t.Fatal("Close did not finish after silent RTP worker exited")
	}
	if !codec.wasClosed() {
		t.Fatal("realtime codec was not closed")
	}
}

func newSilentLifecycleSession(
	t *testing.T,
	codec RealtimeCodec,
	onState func(string, webrtc.PeerConnectionState),
) *MediaSession {
	t.Helper()
	connection, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	target, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		_ = connection.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = target.Close() })
	peer, err := webrtc.NewPeerConnection(webrtc.Configuration{})
	if err != nil {
		_ = connection.Close()
		t.Fatal(err)
	}
	return &MediaSession{
		ID: "media-silence", peer: peer, rtpConn: connection, onState: onState,
		remote: rtpEndpoint{
			Address: target.LocalAddr().(*net.UDPAddr), Codec: "AMR",
			ClockRate: 8000, PayloadType: 114,
		},
		realtimeCodec: codec, attached: true, receiveOnly: true, closed: make(chan struct{}),
	}
}

type failingSilentCodec struct{}

func (failingSilentCodec) SampleRate() int                { return 8000 }
func (failingSilentCodec) Decode([]byte) ([]int16, error) { return nil, errors.New("unused") }
func (failingSilentCodec) Encode([]int16) ([]byte, error) { return nil, errors.New("encode failed") }
func (failingSilentCodec) Close() error                   { return nil }

type encodedSilentCodec struct{}

func (encodedSilentCodec) SampleRate() int                { return 8000 }
func (encodedSilentCodec) Decode([]byte) ([]int16, error) { return nil, errors.New("unused") }
func (encodedSilentCodec) Encode([]int16) ([]byte, error) { return []byte("encoded"), nil }
func (encodedSilentCodec) Close() error                   { return nil }

type blockingSilentCodec struct {
	entered   chan struct{}
	release   chan struct{}
	enterOnce sync.Once
	mu        sync.Mutex
	closed    bool
}

func (*blockingSilentCodec) SampleRate() int { return 8000 }

func (*blockingSilentCodec) Decode([]byte) ([]int16, error) {
	return nil, errors.New("unused")
}

func (codec *blockingSilentCodec) Encode([]int16) ([]byte, error) {
	codec.enterOnce.Do(func() { close(codec.entered) })
	<-codec.release
	return []byte("encoded"), nil
}

func (codec *blockingSilentCodec) Close() error {
	codec.mu.Lock()
	codec.closed = true
	codec.mu.Unlock()
	return nil
}

func (codec *blockingSilentCodec) wasClosed() bool {
	codec.mu.Lock()
	defer codec.mu.Unlock()
	return codec.closed
}
