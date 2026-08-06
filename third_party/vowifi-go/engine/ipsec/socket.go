package ipsec

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

// ResolveUDPAddrAll resolves an ePDG address into every UDP endpoint. The
// address may be an IP literal or a DNS name; port may be numeric or a
// service name. Results are de-duplicated by IP and IPv4-mapped-IPv6
// addresses are converted to plain IPv4.
func ResolveUDPAddrAll(addr, port string) ([]*net.UDPAddr, error) {
	host := strings.TrimSpace(addr)
	if h, p, err := net.SplitHostPort(addr); err == nil {
		host = h
		if p != "" {
			port = p
		}
	}
	p, err := resolveUDPPort(port)
	if err != nil {
		return nil, err
	}
	if ip := net.ParseIP(host); ip != nil {
		return []*net.UDPAddr{{IP: ipv4Compat(ip), Port: p}}, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil {
		return nil, err
	}
	seen := make(map[string]struct{}, len(ips))
	addrs := make([]*net.UDPAddr, 0, len(ips))
	for _, a := range ips {
		ip := ipv4Compat(a.IP)
		key := ip.String()
		if _, dup := seen[key]; dup {
			continue
		}
		seen[key] = struct{}{}
		addrs = append(addrs, &net.UDPAddr{IP: ip, Port: p})
	}
	if len(addrs) == 0 {
		return nil, errors.New("no IP addresses found")
	}
	return addrs, nil
}

// resolveUDPPort converts a numeric or service port string to an int.
func resolveUDPPort(port string) (int, error) {
	if port == "" {
		return 0, errors.New("missing port")
	}
	if n, err := strconv.Atoi(port); err == nil {
		return n, nil
	}
	n, err := net.LookupPort("udp", port)
	if err != nil {
		return 0, err
	}
	return n, nil
}

// ipv4Compat maps IPv4-mapped IPv6 (::ffff:a.b.c.d) to the 4-byte IPv4 form.
func ipv4Compat(ip net.IP) net.IP {
	if v4 := ip.To4(); v4 != nil {
		return v4
	}
	return ip
}

// Stats is a snapshot of a SocketManager's receive counters.
type Stats struct {
	ESPReceived uint64
	IKEReceived uint64
	IKEDropped  uint64
	ESPDropped  uint64
}

// SocketManager is a Transport that exchanges IKE/ESP datagrams directly with
// the ePDG over a local UDP socket.
type SocketManager struct {
	// Receive counters.
	espReceived uint64
	ikeReceived uint64
	ikeDropped  uint64
	espDropped  uint64

	localIP string // informational, for logs
	conn    *net.UDPConn

	sendMu     sync.Mutex // guards remoteAddr / remoteIPs / rrCounter
	localAddr  *net.UDPAddr
	remoteAddr *net.UDPAddr // current remote (updated on NAT rebinding)
	remoteIPs  []*net.UDPAddr
	numRemotes uint32
	rrCounter  uint32

	ikePackets chan []byte
	espPackets chan []byte
	netEvents  chan NetEvent
	stop       chan struct{}
	wg         sync.WaitGroup
}

// NewSocketManager binds a UDP socket to localAddr (an "IP:port" string) and
// resolves the ePDG address remoteHost:remotePort. It returns a ready
// (un-started) transport.
func NewSocketManager(localIP, localAddr, remoteHost, remotePort string) (*SocketManager, error) {
	addrs, err := ResolveUDPAddrAll(remoteHost, remotePort)
	if err != nil {
		return nil, fmt.Errorf("resolve remote address: %w", err)
	}
	if len(addrs) == 0 {
		return nil, errors.New("remote address resolved to zero endpoints")
	}

	network := "udp"
	if addrs[0].IP.To4() == nil {
		network = "udp"
	}
	if localAddr == "" {
		localAddr = ":0"
	}
	lc := net.ListenConfig{Control: reuseSocketOptions}
	pc, err := lc.ListenPacket(context.Background(), network, localAddr)
	if err != nil {
		return nil, fmt.Errorf("listen on %s: %w", localAddr, err)
	}
	conn := pc.(*net.UDPConn)
	local, err := net.ResolveUDPAddr(network, conn.LocalAddr().String())
	if err != nil {
		conn.Close()
		return nil, err
	}

	return &SocketManager{
		localIP:    localIP,
		conn:       conn,
		localAddr:  local,
		remoteAddr: addrs[0],
		remoteIPs:  addrs,
		numRemotes: uint32(len(addrs)),
		ikePackets: make(chan []byte, 100),
		espPackets: make(chan []byte, 1000),
		netEvents:  make(chan NetEvent, 10),
		stop:       make(chan struct{}),
	}, nil
}

// reuseSocketOptions enables SO_REUSEADDR and SO_REUSEPORT on the UDP socket
// (recovered from NewSocketManager.func1).
func reuseSocketOptions(network, address string, c syscall.RawConn) error {
	var serr error
	err := c.Control(func(fd uintptr) {
		if serr = syscall.SetsockoptInt(int(fd), syscall.SOL_SOCKET, syscall.SO_REUSEADDR, 1); serr != nil {
			return
		}
		serr = syscall.SetsockoptInt(int(fd), syscall.SOL_SOCKET, soReusePort, 1)
	})
	if err != nil {
		return err
	}
	return serr
}

// --- Transport channel accessors ---

func (r *SocketManager) IKEPackets() <-chan []byte     { return r.ikePackets }
func (r *SocketManager) ESPPackets() <-chan []byte     { return r.espPackets }
func (r *SocketManager) NetEventsChan() <-chan NetEvent { return r.netEvents }

// ReceiveIKE blocks until the next IKE packet is delivered by the transport,
// returning an error if the transport has been stopped.
func (r *SocketManager) ReceiveIKE() ([]byte, error) {
	pkt, ok := <-r.ikePackets
	if !ok {
		return nil, errors.New("IKE channel closed")
	}
	return pkt, nil
}

// --- Address accessors ---

func (r *SocketManager) LocalIP() net.IP {
	if r.localAddr == nil {
		return nil
	}
	return r.localAddr.IP
}

func (r *SocketManager) LocalPort() uint16 {
	if r.localAddr == nil {
		return 0
	}
	return uint16(r.localAddr.Port)
}

func (r *SocketManager) RemoteIP() net.IP {
	r.sendMu.Lock()
	defer r.sendMu.Unlock()
	if r.remoteAddr == nil {
		return nil
	}
	return r.remoteAddr.IP
}

func (r *SocketManager) RemotePort() uint16 {
	r.sendMu.Lock()
	defer r.sendMu.Unlock()
	if r.remoteAddr == nil {
		return 0
	}
	return uint16(r.remoteAddr.Port)
}

func (r *SocketManager) SetRemotePort(port uint16) {
	r.sendMu.Lock()
	if r.remoteAddr != nil {
		r.remoteAddr.Port = int(port)
	}
	r.sendMu.Unlock()
}

func (r *SocketManager) LocalAddrString() string {
	if r.localAddr == nil {
		return ""
	}
	return r.localAddr.String()
}

func (r *SocketManager) RemoteAddrString() string {
	r.sendMu.Lock()
	defer r.sendMu.Unlock()
	if r.remoteAddr == nil {
		return ""
	}
	return r.remoteAddr.String()
}

// Stats returns a snapshot of the receive counters.
func (r *SocketManager) Stats() Stats {
	return Stats{
		ESPReceived: atomic.LoadUint64(&r.espReceived),
		IKEReceived: atomic.LoadUint64(&r.ikeReceived),
		IKEDropped:  atomic.LoadUint64(&r.ikeDropped),
		ESPDropped:  atomic.LoadUint64(&r.espDropped),
	}
}

// --- Lifecycle ---

// Start launches the receive loop and the ICMP error listener.
func (r *SocketManager) Start() {
	r.wg.Add(1)
	go r.readLoop()
	r.wg.Add(1)
	go r.startErrorListener()
}

// Stop tears the transport down: it closes the socket, waits for the loops to
// finish and closes the delivery channels.
func (r *SocketManager) Stop() {
	select {
	case <-r.stop:
	default:
		close(r.stop)
	}
	if r.conn != nil {
		r.conn.Close()
	}
	r.wg.Wait()
	close(r.ikePackets)
	close(r.espPackets)
	close(r.netEvents)
}

// readLoop receives datagrams, tracks NAT rebinding and dispatches IKE/ESP
// packets to the delivery channels.
func (r *SocketManager) readLoop() {
	defer r.wg.Done()
	buf := make([]byte, 0x1000)
	for {
		n, from, err := r.conn.ReadFromUDP(buf)
		if err != nil {
			return
		}
		pkt := buf[:n]

		// Track the source: a packet from an IP outside the configured list
		// (or on a new port) signals NAT rebinding.
		r.sendMu.Lock()
		known := r.numRemotes == 0
		for i := 0; i < int(r.numRemotes) && i < len(r.remoteIPs); i++ {
			if r.remoteIPs[i].IP.Equal(from.IP) {
				if r.numRemotes > 1 {
					// Promote the matching IP to primary and collapse the list.
					r.remoteIPs[0], r.remoteIPs[i] = r.remoteIPs[i], r.remoteIPs[0]
					r.remoteIPs = r.remoteIPs[:1]
					r.numRemotes = 1
					logDebug("remote endpoint switched to " + from.IP.String())
				}
				known = true
				break
			}
		}
		if known && from.Port != r.remoteAddr.Port && from.Port > 0 {
			oldPort := uint32(r.remoteAddr.Port)
			r.remoteAddr.Port = from.Port
			detail := fmt.Sprintf("updated remote port from %d to %d", oldPort, from.Port)
			logInfo(detail)
			select {
			case r.netEvents <- NetEvent{Type: NetEventPortChanged, OldPort: oldPort, NewPort: uint32(from.Port), Detail: detail}:
			default:
			}
		}
		r.sendMu.Unlock()

		// A single 0xff byte is a NAT keepalive and is not forwarded.
		if n == 1 && pkt[0] == 0xff {
			continue
		}

		if ikePkt, ok := parseIKEPayload(pkt, n); ok {
			select {
			case r.ikePackets <- ikePkt:
				atomic.AddUint64(&r.ikeReceived, 1)
			default:
				atomic.AddUint64(&r.ikeDropped, 1)
				logWarn("IKE packet dropped (queue full)")
			}
			continue
		}

		// ESP: strip a defensive 4-byte non-ESP marker if present.
		esp := pkt
		if n > 4 && esp[0] == 0 && esp[1] == 0 && esp[2] == 0 && esp[3] == 0 {
			esp = esp[4:]
		}
		if len(esp) == 0 {
			continue
		}
		select {
		case r.espPackets <- esp:
			atomic.AddUint64(&r.espReceived, 1)
		default:
			atomic.AddUint64(&r.espDropped, 1)
			logWarn("ESP packet dropped (queue full)")
		}
	}
}

// --- Send paths ---

// SendIKE sends an IKE packet, prepending the RFC 3948 non-ESP marker when
// talking to port 4500.
func (r *SocketManager) SendIKE(packet []byte) {
	r.sendMu.Lock()
	if r.numRemotes > 1 {
		idx := r.rrCounter % r.numRemotes
		r.rrCounter++
		*r.remoteAddr = *r.remoteIPs[idx]
		logDebug("sending IKE to " + r.remoteAddr.IP.String())
	}
	addr := *r.remoteAddr
	r.sendMu.Unlock()

	out := packet
	if addr.Port == 4500 {
		out = append([]byte{0, 0, 0, 0}, out...)
	}
	if _, err := r.conn.WriteToUDP(out, &addr); err != nil {
		logWarn("failed to send IKE packet to " + addr.String() + ": " + err.Error())
	}
}

// SendESP sends an ESP packet.
func (r *SocketManager) SendESP(packet []byte) {
	r.sendMu.Lock()
	addr := *r.remoteAddr
	r.sendMu.Unlock()

	if _, err := r.conn.WriteToUDP(packet, &addr); err != nil {
		if errors.Is(err, net.ErrClosed) {
			return
		}
		if strings.Contains(err.Error(), "use of closed network connection") {
			return
		}
		logWarn(fmt.Sprintf("failed to send ESP packet to %s: %v, len %d", addr.String(), err, len(packet)))
	}
}

// SendNATKeepalive sends the RFC 3948 keepalive (a single 0xff byte).
func (r *SocketManager) SendNATKeepalive() {
	r.sendMu.Lock()
	addr := *r.remoteAddr
	r.sendMu.Unlock()

	if _, err := r.conn.WriteToUDP([]byte{0xff}, &addr); err != nil {
		if errors.Is(err, net.ErrClosed) || strings.Contains(err.Error(), "use of closed network connection") {
			return
		}
		logWarn(fmt.Sprintf("failed to send NAT keepalive to %s: %v, local addr %s", addr.String(), err, r.LocalAddrString()))
	}
}

// RawFD returns the underlying socket file descriptor.
func (r *SocketManager) RawFD() (int, error) {
	if r.conn == nil {
		return -1, errors.New("socket not created")
	}
	raw, err := r.conn.SyscallConn()
	if err != nil {
		return -1, err
	}
	var fd int
	var ctrlErr error
	err = raw.Control(func(f uintptr) { fd = int(f) })
	if err != nil {
		return -1, err
	}
	if ctrlErr != nil {
		return -1, ctrlErr
	}
	return fd, nil
}

// SetUDPEncap enables or disables UDP encapsulation (ESP-in-UDP) on the
// socket.
func (r *SocketManager) SetUDPEncap(enable bool) error {
	if r.conn == nil {
		return errors.New("socket not created")
	}
	if err := setUDPEncap(r.conn, enable); err != nil {
		return err
	}
	logInfo(fmt.Sprintf("UDP encapsulation %v, local addr %s", enable, r.LocalAddrString()))
	return nil
}

// setUDPEncap is implemented in the platform-specific file.
