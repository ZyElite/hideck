// Package media relays RTP and RTCP between IMS and a local voice client.
package media

import (
	"net"
	"os"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/iniwex5/vowifi-go/internal/vowifi/imsendpoint"
)

const (
	MediaEventRTPTimeout MediaEvent = iota + 1
	MediaEventOneWayTimeout
)

// MediaEvent is emitted by MediaSessionManager when media health changes.
type MediaEvent int

type ptMapping struct {
	imsToLan map[int]int
	lanToIms map[int]int
}

// RTPRelay owns the RTP/RTCP sockets for one call.
type RTPRelay struct {
	bytesIMSToLAN     uint64
	bytesLANToIMS     uint64
	bytesIMSRTCPToLAN uint64
	bytesLANRTCPToIMS uint64

	connIMS     net.PacketConn
	connLAN     *net.UDPConn
	connIMSRTCP net.PacketConn
	connLANRTCP *net.UDPConn

	remoteAddr     atomic.Pointer[net.UDPAddr]
	remoteAddrRTCP atomic.Pointer[net.UDPAddr]
	clientAddr     atomic.Pointer[net.UDPAddr]
	clientAddrRTCP atomic.Pointer[net.UDPAddr]

	stopCh   chan struct{}
	stopOnce sync.Once
	mu       sync.RWMutex
	active   bool

	imsFirstPacket     uint32
	lanFirstPacket     uint32
	imsRTCPFirstPacket uint32
	lanRTCPFirstPacket uint32

	Monitor            *RTPMonitor
	rtcpKeepaliveTimer *time.Timer
	rtcpMu             sync.Mutex
	ptMap              atomic.Pointer[ptMapping]
	deviceID           string
	traceID            string
	pcapFile           *os.File
	pcapMu             sync.Mutex
	pcapEnable         bool

	wg             sync.WaitGroup
	rtcpWG         sync.WaitGroup
	monitorStarted bool
	lanPacket      net.PacketConn
	pcapWriter     packetCaptureWriter
	pcapErr        error
	pcapPath       string
	audioRecorder  *rtpAudioRecorder
	audioTarget    string
	audioPath      string
	audioCodec     string
	audioErr       error
	captureErr     error
	stopErr        error
	imsRemote      *net.UDPAddr
	lanRemote      *net.UDPAddr

	dtmfMu              sync.Mutex
	dtmfSendMu          sync.Mutex
	dtmfWriteMu         sync.Mutex
	dtmfWG              sync.WaitGroup
	dtmfPayloadType     int
	dtmfClockRate       int
	dtmfEventMask       uint16
	dtmfSequence        uint16
	dtmfTimestamp       uint32
	dtmfSSRC            uint32
	dtmfSeedErr         error
	dtmfSourceObserved  bool
	dtmfSending         bool
	dtmfRewritePending  bool
	dtmfSequenceOffset  uint16
	dtmfLastRTPPacketAt time.Time
}

// RTPMonitor stores monotonic media activity timestamps as Unix nanoseconds.
type RTPMonitor struct {
	mu                       sync.RWMutex
	LastActivity             atomic.Int64
	LastIMSToLAN             atomic.Int64
	LastLANToIMS             atomic.Int64
	Timeout                  int64
	OnTimeout                func()
	OnOneWayTimeout          func(string)
	imsToLanTimeoutTriggered bool
	lanToImsTimeoutTriggered bool
	stopMonitor              chan struct{}
	stopOnce                 sync.Once
	imsCount                 atomic.Uint64
	lanCount                 atomic.Uint64
}

// MediaSessionManager owns one original relay and additive call-keyed relays.
type MediaSessionManager struct {
	mu       sync.Mutex
	relay    *RTPRelay
	deviceID string
	traceID  string
	EventCh  chan MediaEvent
	released bool

	relays map[string]*RTPRelay
}

// Bridge creates relays from an IMS runtime endpoint.
type Bridge struct {
	deviceID string
	endpoint imsendpoint.RuntimeSnapshotSource

	mu             sync.RWMutex
	relay          *RTPRelay
	legacyEndpoint string
}

// ComfortNoiseGenerator emits 20 ms PCMU RTP packets.
type ComfortNoiseGenerator struct {
	conn       net.PacketConn
	remoteAddr *net.UDPAddr
	seqNum     uint16
	timestamp  uint32
	ssrc       uint32
	seed       uint32
	stopCh     chan struct{}
	stopOnce   sync.Once
	wg         sync.WaitGroup
	deviceID   string
	traceID    string

	mu      sync.Mutex
	errors  chan error
	started bool
}

type packetCaptureWriter interface {
	Write([]byte) (int, error)
	Close() error
}

// AudioCodec describes one negotiated RTP audio payload.
type AudioCodec struct {
	PayloadType int
	Name        string
	ClockRate   int
	Channels    int
	Fmtp        string
}

// CaptureSnapshot reports durable files produced for one call.
type CaptureSnapshot struct {
	PCAPPath  string
	AudioPath string
	Codec     string
	Err       error
}

type syscallPacketConn interface {
	SyscallConn() (syscall.RawConn, error)
}
