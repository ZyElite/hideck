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

// Socks5Transport is a Transport that carries IKE/ESP traffic over a SOCKS5
// UDP associate relay (RFC 1928 §7). It is used when the device has no direct
// route to the ePDG and must traverse a SOCKS5 proxy.
type Socks5Transport struct {
	// Receive counters.
	totalReceived  uint64
	invalidDropped uint64
	natKeepalives  uint64
	lastPacketLen  uint64
	espReceived    uint64
	ikeReceived    uint64
	espForwarded   uint64
	ikeDropped     uint64
	espDropped     uint64

	targetStr  string           // ePDG "IP:port", for logs
	clientAddr *net.UDPAddr     // ePDG endpoint announced in datagrams
	mu         sync.RWMutex     // guards remotePort
	remotePort uint16

	tcpConn   net.Conn         // SOCKS5 control connection
	udpConn   *net.UDPConn     // local UDP socket talking to the relay
	relayAddr *net.UDPAddr     // relay returned by UDP associate
	localIP   net.IP
	localPort uint16

	ikePackets chan []byte
	espPackets chan []byte
	netEvents  chan NetEvent
	stop       chan struct{}
	stopOnce   sync.Once
	wg         sync.WaitGroup
}

// NewSocks5Transport connects to a SOCKS5 proxy, performs the handshake and
// UDP associate, and returns a ready transport. targetAddr is the ePDG
// endpoint; config carries optional credentials.
func NewSocks5Transport(config Socks5Config, socks5Addr string, targetAddr string, timeout time.Duration) (*Socks5Transport, error) {
	addrs, err := ResolveUDPAddrAll(targetAddr, "")
	if err != nil {
		return nil, fmt.Errorf("resolve target address: %w", err)
	}
	if len(addrs) == 0 {
		return nil, errors.New("target address resolved to zero endpoints")
	}
	clientAddr := addrs[0]

	host, port, err := parseSocks5Addr(socks5Addr)
	if err != nil {
		return nil, fmt.Errorf("parse socks5 address: %w", err)
	}
	proxyAddr := net.JoinHostPort(host, strconv.Itoa(port))
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	conn, err := net.DialTimeout("tcp", proxyAddr, timeout)
	if err != nil {
		return nil, fmt.Errorf("connect to socks5 proxy %s: %w", proxyAddr, err)
	}

	if err := socks5Handshake(conn, &config); err != nil {
		conn.Close()
		return nil, fmt.Errorf("socks5 handshake: %w", err)
	}
	clientForReq := socks5UDPAssociateClientAddr(conn)
	relay, err := socks5UDPAssociate(conn, &config, clientForReq)
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("socks5 udp associate: %w", err)
	}

	lc := net.ListenConfig{Control: reuseSocketOptions}
	pc, err := lc.ListenPacket(context.Background(), "udp", ":0")
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("bind udp relay socket: %w", err)
	}
	udpConn := pc.(*net.UDPConn)
	local := udpConn.LocalAddr().(*net.UDPAddr)

	t := &Socks5Transport{
		targetStr:  clientAddr.String(),
		clientAddr: clientAddr,
		remotePort: uint16(clientAddr.Port),
		tcpConn:    conn,
		udpConn:    udpConn,
		relayAddr:  relay,
		localIP:    ipv4Compat(local.IP),
		localPort:  uint16(local.Port),
		ikePackets: make(chan []byte, 100),
		espPackets: make(chan []byte, 1000),
		netEvents:  make(chan NetEvent, 10),
		stop:       make(chan struct{}),
	}
	return t, nil
}

// socks5UDPAssociateClientAddr computes the address announced in the UDP
// associate request: the local IP of the TCP connection (or 0.0.0.0).
func socks5UDPAssociateClientAddr(conn net.Conn) *net.UDPAddr {
	if conn != nil {
		if la := conn.LocalAddr(); la != nil {
			if addr, ok := la.(*net.TCPAddr); ok && addr.IP != nil && !addr.IP.IsUnspecified() {
				return &net.UDPAddr{IP: ipv4Compat(addr.IP), Port: 0}
			}
		}
	}
	return &net.UDPAddr{IP: net.IPv4zero, Port: 0}
}

// --- Transport accessors ---

func (t *Socks5Transport) IKEPackets() <-chan []byte      { return t.ikePackets }
func (t *Socks5Transport) ESPPackets() <-chan []byte      { return t.espPackets }
func (t *Socks5Transport) NetEventsChan() <-chan NetEvent { return t.netEvents }

func (t *Socks5Transport) LocalIP() net.IP {
	return t.localIP
}

func (t *Socks5Transport) RemoteIP() net.IP {
	t.mu.RLock()
	defer t.mu.RUnlock()
	if t.clientAddr == nil {
		return nil
	}
	return t.clientAddr.IP
}

func (t *Socks5Transport) LocalPort() uint16 { return t.localPort }

func (t *Socks5Transport) RemotePort() uint16 {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.remotePort
}

func (t *Socks5Transport) SetRemotePort(port uint16) {
	t.mu.Lock()
	t.remotePort = port
	t.mu.Unlock()
	logDebug(fmt.Sprintf("remote port set to %d (target %s)", port, t.targetStr))
}

func (t *Socks5Transport) LocalAddrString() string {
	return fmt.Sprintf("%s:%d", t.localIP.String(), t.localPort)
}

func (t *Socks5Transport) RemoteAddrString() string {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return fmt.Sprintf("%s:%d", t.clientAddr.IP.String(), t.remotePort)
}

// RawFD is not applicable to a SOCKS5 transport.
func (t *Socks5Transport) RawFD() (int, error) {
	return -1, errors.New("socks5 transport has no raw file descriptor")
}

// Stats is a no-op on the SOCKS5 transport (the original returns nothing;
// counters are logged periodically by logStatsLoop).
func (t *Socks5Transport) Stats() {}

// SetUDPEncap is not supported over SOCKS5.
func (t *Socks5Transport) SetUDPEncap(enable bool) error {
	return errors.New("udp encapsulation is not supported on socks5 transport")
}

// --- Lifecycle ---

// Start launches the datagram receive loop, the statistics logger and the TCP
// keepalive goroutine.
func (t *Socks5Transport) Start() {
	t.wg.Add(3)
	go t.readLoop()
	go t.logStatsLoop()
	go t.tcpKeepalive()
}

// Stop tears the transport down.
func (t *Socks5Transport) Stop() {
	t.stopOnce.Do(func() {
		close(t.stop)
		if t.tcpConn != nil {
			t.tcpConn.Close()
		}
		if t.udpConn != nil {
			t.udpConn.Close()
		}
		t.wg.Wait()
		close(t.ikePackets)
		close(t.espPackets)
		close(t.netEvents)
	})
}

// --- Send paths ---

// SendIKE sends an IKE packet, prepending the RFC 3948 non-ESP marker when
// talking to port 4500.
func (t *Socks5Transport) SendIKE(packet []byte) {
	t.mu.RLock()
	port := t.remotePort
	t.mu.RUnlock()
	if port == 4500 {
		packet = append([]byte{0, 0, 0, 0}, packet...)
	}
	t.sendUDP(packet)
}

// SendESP sends an ESP packet.
func (t *Socks5Transport) SendESP(packet []byte) {
	t.sendUDP(packet)
}

// SendNATKeepalive sends the RFC 3948 keepalive (a single 0xff byte).
func (t *Socks5Transport) SendNATKeepalive() {
	t.sendUDP([]byte{0xff})
}

// sendUDP wraps data in a SOCKS5 UDP datagram addressed to the ePDG and sends
// it to the relay.
func (t *Socks5Transport) sendUDP(data []byte) error {
	t.mu.RLock()
	addr := &net.UDPAddr{IP: t.clientAddr.IP, Port: int(t.remotePort)}
	t.mu.RUnlock()
	dgram := EncodeSocks5UDPDatagram(addr, data)
	if _, err := t.udpConn.WriteToUDP(dgram, t.relayAddr); err != nil {
		return fmt.Errorf("failed to send UDP datagram to relay %s: %w", t.relayAddr, err)
	}
	return nil
}

// readLoop receives relayed datagrams and dispatches IKE/ESP packets.
func (t *Socks5Transport) readLoop() {
	defer t.wg.Done()
	buf := make([]byte, 0xffff)
	for {
		n, _, err := t.udpConn.ReadFromUDP(buf)
		if err != nil {
			return
		}
		atomic.AddUint64(&t.totalReceived, 1)
		atomic.StoreUint64(&t.lastPacketLen, uint64(n))

		dgram, err := DecodeSocks5UDPDatagram(buf[:n])
		if err != nil || dgram.Frag != 0 {
			atomic.AddUint64(&t.invalidDropped, 1)
			logWarn(fmt.Sprintf("invalid socks5 udp datagram (frag=%d, len=%d): %v", dgram.Frag, n, err))
			continue
		}
		data := dgram.Data
		// NAT keepalive: empty or a single 0xff byte.
		if len(data) == 0 || (len(data) == 1 && data[0] == 0xff) {
			atomic.AddUint64(&t.natKeepalives, 1)
			continue
		}
		// Strip a defensive 4-byte non-ESP marker.
		if len(data) > 4 && data[0] == 0 && data[1] == 0 && data[2] == 0 && data[3] == 0 {
			data = data[4:]
		}

		if ikePkt, ok := parseIKEPayload(data, n); ok {
			atomic.AddUint64(&t.ikeReceived, 1)
			pkt := append([]byte{}, ikePkt...)
			select {
			case t.ikePackets <- pkt:
			default:
				atomic.AddUint64(&t.ikeDropped, 1)
				logWarn("IKE packet dropped (queue full)")
			}
			continue
		}
		if len(data) == 0 {
			continue
		}
		atomic.AddUint64(&t.espReceived, 1)
		pkt := append([]byte{}, data...)
		select {
		case t.espPackets <- pkt:
			atomic.AddUint64(&t.espForwarded, 1)
		default:
			atomic.AddUint64(&t.espDropped, 1)
			logWarn("ESP packet dropped (queue full)")
		}
	}
}

// logStatsLoop periodically logs the receive counters.
func (t *Socks5Transport) logStatsLoop() {
	defer t.wg.Done()
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-t.stop:
			return
		case <-ticker.C:
			logDebug(fmt.Sprintf(
				"socks5 transport stats: total=%d invalid=%d keepalive=%d last=%d ike=%d esp=%d ikeDrop=%d espDrop=%d",
				atomic.LoadUint64(&t.totalReceived), atomic.LoadUint64(&t.invalidDropped),
				atomic.LoadUint64(&t.natKeepalives), atomic.LoadUint64(&t.lastPacketLen),
				atomic.LoadUint64(&t.ikeReceived), atomic.LoadUint64(&t.espReceived),
				atomic.LoadUint64(&t.ikeDropped), atomic.LoadUint64(&t.espDropped)))
		}
	}
}

// tcpKeepalive keeps the SOCKS5 control connection alive and detects when the
// proxy drops it.
func (t *Socks5Transport) tcpKeepalive() {
	defer t.wg.Done()
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-t.stop:
			return
		case <-ticker.C:
		}
		if t.tcpConn == nil {
			return
		}
		if err := t.tcpConn.SetWriteDeadline(time.Now().Add(5 * time.Second)); err != nil {
			continue
		}
		if _, err := t.tcpConn.Write([]byte{0}); err != nil {
			if ne, ok := err.(net.Error); ok && ne.Timeout() {
				continue // transient; keep the connection
			}
			if errors.Is(err, net.ErrClosed) {
				return
			}
			logWarn(fmt.Sprintf("TCP keepalive to %s failed: %v", t.targetStr, err))
			select {
			case t.netEvents <- NetEvent{Type: NetEventUnreachable, Detail: err.Error()}:
			default:
			}
			return
		}
	}
}
