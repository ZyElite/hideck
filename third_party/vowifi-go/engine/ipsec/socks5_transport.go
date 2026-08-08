package ipsec

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strconv"
	"sync"
	"sync/atomic"
	"time"
)

const (
	defaultSocks5Timeout = 10 * time.Second
	socksIKEQueueSize    = 128
	socksESPQueueSize    = 512
	socksEventQueueSize  = 16
)

// Socks5TransportStats is a snapshot of the legacy SOCKS5 counters.
type Socks5TransportStats struct {
	UDPReadTotal        uint64
	UDPDecodeErrorTotal uint64
	UDPFragDropTotal    uint64
	NATKeepaliveDrop    uint64
	LastUDPReadLen      uint64
	LastESPReadLen      uint64
	ReceivedIKETotal    uint64
	ReceivedESPTotal    uint64
	DroppedIKETotal     uint64
	DroppedESPTotal     uint64
}

// Socks5Transport carries IKE and ESP over an RFC 1928 UDP association.
type Socks5Transport struct {
	udpReadTotal        uint64
	udpDecodeErrorTotal uint64
	udpFragDropTotal    uint64
	natKeepaliveDrop    uint64
	lastUDPReadLen      uint64
	lastESPReadLen      uint64
	receivedIKE         uint64
	receivedESP         uint64
	droppedIKE          uint64
	droppedESP          uint64

	cfg        Socks5Config
	tcpConn    net.Conn
	udpConn    *net.UDPConn
	relayAddr  *net.UDPAddr
	remoteIP   net.IP
	remotePort int
	remoteMu   sync.RWMutex
	localIP    net.IP
	localPort  uint16
	ikeChan    chan []byte
	espChan    chan []byte
	netEvents  chan NetEvent
	ctx        context.Context
	cancel     context.CancelFunc
	wg         sync.WaitGroup
	stopOnce   sync.Once
	lifecycle  sync.Mutex
	started    bool
	stopped    bool
	startErr   error
}

type socks5Connections struct {
	tcp   net.Conn
	udp   *net.UDPConn
	relay *net.UDPAddr
}

// NewSocks5Transport creates the legacy config-based transport. The optional
// arguments retain source compatibility with the earlier reconstructed API:
// proxy address, remote address, and dial timeout.
func NewSocks5Transport(cfg Socks5Config, compatibility ...any) (*Socks5Transport, error) {
	timeout, err := applySocks5Compatibility(&cfg, compatibility)
	if err != nil {
		return nil, err
	}
	remoteAddr, _, err := ResolveUDPAddrAll(cfg.RemoteAddr, cfg.DNSServer)
	if err != nil {
		return nil, fmt.Errorf("resolve SOCKS5 target %q: %w", cfg.RemoteAddr, err)
	}
	tcpConn, err := connectSocks5(cfg, timeout)
	if err != nil {
		return nil, err
	}
	relayAddr, err := establishUDPAssociation(tcpConn, &cfg)
	if err != nil {
		_ = tcpConn.Close()
		return nil, err
	}
	udpConn, err := net.ListenUDP("udp", nil)
	if err != nil {
		_ = tcpConn.Close()
		return nil, fmt.Errorf("create SOCKS5 UDP socket: %w", err)
	}
	connections := socks5Connections{tcp: tcpConn, udp: udpConn, relay: relayAddr}
	return newSocks5Transport(cfg, connections, remoteAddr), nil
}

func applySocks5Compatibility(cfg *Socks5Config, values []any) (time.Duration, error) {
	timeout := defaultSocks5Timeout
	if len(values) == 0 {
		return timeout, nil
	}
	if len(values) != 3 {
		return 0, errors.New("SOCKS5 compatibility constructor requires proxy, target, and timeout")
	}
	proxy, proxyOK := values[0].(string)
	target, targetOK := values[1].(string)
	duration, durationOK := values[2].(time.Duration)
	if !proxyOK || !targetOK || !durationOK {
		return 0, errors.New("invalid SOCKS5 compatibility constructor arguments")
	}
	cfg.ProxyAddr, cfg.RemoteAddr = proxy, target
	if duration > 0 {
		timeout = duration
	}
	return timeout, nil
}

func connectSocks5(cfg Socks5Config, timeout time.Duration) (net.Conn, error) {
	host, port, err := parseSocks5Addr(cfg.ProxyAddr)
	if err != nil {
		return nil, fmt.Errorf("parse SOCKS5 address %q: %w", cfg.ProxyAddr, err)
	}
	address := net.JoinHostPort(host, strconv.Itoa(port))
	conn, err := net.DialTimeout("tcp", address, timeout)
	if err != nil {
		return nil, fmt.Errorf("connect to SOCKS5 proxy %s: %w", address, err)
	}
	return conn, nil
}

func establishUDPAssociation(conn net.Conn, cfg *Socks5Config) (*net.UDPAddr, error) {
	if err := socks5Handshake(conn, cfg); err != nil {
		return nil, fmt.Errorf("SOCKS5 handshake: %w", err)
	}
	relay, err := socks5UDPAssociate(conn, socks5UDPAssociateClientAddr(conn))
	if err != nil {
		return nil, fmt.Errorf("SOCKS5 UDP associate: %w", err)
	}
	if relay.IP.IsUnspecified() {
		if remote, ok := conn.RemoteAddr().(*net.TCPAddr); ok {
			relay.IP = append(net.IP(nil), remote.IP...)
		}
	}
	return relay, nil
}

func newSocks5Transport(
	cfg Socks5Config,
	connections socks5Connections,
	remoteAddr *net.UDPAddr,
) *Socks5Transport {
	localAddr := connections.udp.LocalAddr().(*net.UDPAddr)
	ctx, cancel := context.WithCancel(context.Background())
	return &Socks5Transport{
		cfg: cfg, tcpConn: connections.tcp, udpConn: connections.udp, relayAddr: connections.relay,
		remoteIP: append(net.IP(nil), remoteAddr.IP...), remotePort: remoteAddr.Port,
		localIP: append(net.IP(nil), localAddr.IP...), localPort: uint16(localAddr.Port),
		ikeChan: make(chan []byte, socksIKEQueueSize), espChan: make(chan []byte, socksESPQueueSize),
		netEvents: make(chan NetEvent, socksEventQueueSize), ctx: ctx, cancel: cancel,
	}
}

func socks5UDPAssociateClientAddr(conn net.Conn) *net.UDPAddr {
	if conn != nil {
		if addr, ok := conn.LocalAddr().(*net.TCPAddr); ok && addr.IP != nil && !addr.IP.IsUnspecified() {
			return &net.UDPAddr{IP: ipv4Compat(addr.IP)}
		}
	}
	return &net.UDPAddr{IP: net.IPv4zero}
}

func (t *Socks5Transport) IKEPackets() <-chan []byte      { return t.ikeChan }
func (t *Socks5Transport) ESPPackets() <-chan []byte      { return t.espChan }
func (t *Socks5Transport) NetEventsChan() <-chan NetEvent { return t.netEvents }
func (t *Socks5Transport) LocalIP() net.IP                { return append(net.IP(nil), t.localIP...) }
func (t *Socks5Transport) LocalPort() uint16              { return t.localPort }

func (t *Socks5Transport) RemoteIP() net.IP {
	t.remoteMu.RLock()
	defer t.remoteMu.RUnlock()
	return append(net.IP(nil), t.remoteIP...)
}

func (t *Socks5Transport) RemotePort() int {
	t.remoteMu.RLock()
	defer t.remoteMu.RUnlock()
	return t.remotePort
}

func (t *Socks5Transport) SetRemotePort(port int) {
	t.remoteMu.Lock()
	t.remotePort = port
	t.remoteMu.Unlock()
	logDebug(fmt.Sprintf("SOCKS5 remote port set to %d for %s", port, t.cfg.DeviceID))
}

func (t *Socks5Transport) LocalAddrString() string {
	return fmt.Sprintf("%s:%d (via socks5 %s)", t.localIP, t.localPort, t.cfg.ProxyAddr)
}

func (t *Socks5Transport) RemoteAddrString() string {
	t.remoteMu.RLock()
	defer t.remoteMu.RUnlock()
	return fmt.Sprintf("%s:%d", t.remoteIP, t.remotePort)
}

func (*Socks5Transport) RawFD() (int, error) {
	return -1, errors.New("SOCKS5 transport has no raw file descriptor")
}

func (*Socks5Transport) SetUDPEncap() error {
	return errors.New("UDP encapsulation is not supported on SOCKS5 transport")
}

// Stats returns the legacy ten-counter snapshot without resetting it.
func (t *Socks5Transport) Stats() Socks5TransportStats {
	return Socks5TransportStats{
		UDPReadTotal:        atomic.LoadUint64(&t.udpReadTotal),
		UDPDecodeErrorTotal: atomic.LoadUint64(&t.udpDecodeErrorTotal),
		UDPFragDropTotal:    atomic.LoadUint64(&t.udpFragDropTotal),
		NATKeepaliveDrop:    atomic.LoadUint64(&t.natKeepaliveDrop),
		LastUDPReadLen:      atomic.LoadUint64(&t.lastUDPReadLen),
		LastESPReadLen:      atomic.LoadUint64(&t.lastESPReadLen),
		ReceivedIKETotal:    atomic.LoadUint64(&t.receivedIKE),
		ReceivedESPTotal:    atomic.LoadUint64(&t.receivedESP),
		DroppedIKETotal:     atomic.LoadUint64(&t.droppedIKE),
		DroppedESPTotal:     atomic.LoadUint64(&t.droppedESP),
	}
}

// SnapshotStats retains the reconstructed accessor name.
func (t *Socks5Transport) SnapshotStats() Socks5TransportStats { return t.Stats() }
