// Package media implements the RTP/RTCP relay between the IMS network and
// the local client (RFC 3550, RFC 3551). It relays RTP packets, maps payload
// types, generates comfort noise, and monitors one-way media timeouts.
//
// Reconstructed from the decompiled internal/vowifi/voice/media.
package media

import (
	"net"
	"sync"
	"time"
)

// RTPRelay relays RTP/RTCP between the IMS side and the LAN (client) side.
type RTPRelay struct {
	mu sync.RWMutex

	imsConn net.PacketConn
	lanConn net.PacketConn

	imsRemote *net.UDPAddr
	lanRemote *net.UDPAddr

	ptMapping map[int]int // LAN PT -> IMS PT

	monitor *RTPMonitor

	oneWayTimeout time.Duration
	onOneWay      func()

	stop    chan struct{}
	stopped bool

	logContext string
	pcap       *pcapWriter
}

// MediaSessionManager owns the media relays for one device.
type MediaSessionManager struct {
	mu     sync.RWMutex
	relays map[string]*RTPRelay // keyed by call ID
}

// Bridge is the media bridge between the client and the IMS network.
type Bridge struct {
	mu       sync.RWMutex
	endpoint string
	relay    *RTPRelay
}

// ComfortNoiseGenerator generates comfort noise (RFC 3389) when media is
// one-way or muted.
type ComfortNoiseGenerator struct {
	mu      sync.Mutex
	conn    net.PacketConn
	addr    *net.UDPAddr
	stop    chan struct{}
	started bool
}

// RTPMonitor tracks RTP packet flow to detect one-way media.
type RTPMonitor struct {
	mu          sync.Mutex
	lastIMSPkt  time.Time
	lastLANPkt  time.Time
	imsCount    uint64
	lanCount    uint64
	oneWaySince time.Time
}

// pcapWriter writes RTP packets to a pcap file for debugging.
type pcapWriter struct {
	mu   sync.Mutex
	file osFile
}

// osFile is the file interface used by the pcap writer.
type osFile interface {
	Write(p []byte) (int, error)
	Close() error
}
