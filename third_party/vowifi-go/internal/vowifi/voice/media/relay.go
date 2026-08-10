package media

import (
	"errors"
	"fmt"
	"net"
	"strings"
	"sync/atomic"
	"time"

	"github.com/iniwex5/vowifi-go/internal/vowifi/imsendpoint"
	"github.com/iniwex5/vowifi-go/internal/vowifi/logging"
)

const (
	mediaReadBufferSize = 64 * 1024
	monitorInterval     = 5 * time.Second
	rtcpKeepalive       = 10 * time.Second
)

// NewRTPRelay retains the additive constructor for already-open RTP sockets.
func NewRTPRelay(imsConn, lanConn net.PacketConn) *RTPRelay {
	relay := newRTPRelay(imsConn, lanConn, nil, nil)
	relay.lanPacket = lanConn
	if udp, ok := lanConn.(*net.UDPConn); ok {
		relay.connLAN = udp
	}
	return relay
}

func newRTPRelay(
	imsRTP, lanRTP net.PacketConn,
	imsRTCP net.PacketConn,
	lanRTCP *net.UDPConn,
) *RTPRelay {
	relay := &RTPRelay{
		connIMS: imsRTP, connIMSRTCP: imsRTCP, connLANRTCP: lanRTCP,
		lanPacket: lanRTP, stopCh: make(chan struct{}), dtmfPayloadType: -1,
	}
	relay.ptMap.Store(&ptMapping{imsToLan: map[int]int{}, lanToIms: map[int]int{}})
	relay.seedDTMF()
	return relay
}

// NewRTPRelayWithListener accepts both the original listener form
// (PacketListener, IMS address, LAN address, range start, range end) and the
// additive one-address form used by earlier restoration builds.
func NewRTPRelayWithListener(source any, args ...any) (*RTPRelay, error) {
	if address, ok := source.(*net.UDPAddr); ok && len(args) == 0 {
		return newRelayFromAddresses(nil, address.String(), address.IP.String(), 0, 0)
	}
	listener, ok := source.(imsendpoint.PacketListener)
	if !ok && source != nil {
		return nil, fmt.Errorf("media: unsupported IMS packet listener %T", source)
	}
	if len(args) != 4 {
		return nil, errors.New("media: listener constructor requires IMS address, LAN address and port range")
	}
	imsAddress, okIMS := args[0].(string)
	lanAddress, okLAN := args[1].(string)
	start, okStart := args[2].(int)
	end, okEnd := args[3].(int)
	if !okIMS || !okLAN || !okStart || !okEnd {
		return nil, errors.New("media: invalid listener constructor arguments")
	}
	return newRelayFromAddresses(listener, imsAddress, lanAddress, start, end)
}

func newRelayFromAddresses(
	listener imsendpoint.PacketListener,
	imsAddress, lanAddress string,
	start, end int,
) (*RTPRelay, error) {
	imsIP, err := resolveBindIP(imsAddress)
	if err != nil {
		return nil, err
	}
	imsRTP, err := listenIMSPacket(listener, &net.UDPAddr{IP: imsIP})
	if err != nil {
		return nil, fmt.Errorf("media: bind IMS RTP: %w", err)
	}
	imsRTCP, err := listenIMSPacket(listener, &net.UDPAddr{IP: imsIP})
	if err != nil {
		_ = imsRTP.Close()
		return nil, fmt.Errorf("media: bind IMS RTCP: %w", err)
	}
	lanRTP, lanRTCP, err := listenLANRTPPair(lanAddress, start, end)
	if err != nil {
		_ = imsRTP.Close()
		_ = imsRTCP.Close()
		return nil, err
	}
	relay := newRTPRelay(imsRTP, lanRTP, imsRTCP, lanRTCP)
	relay.connLAN = lanRTP
	for _, conn := range []net.PacketConn{imsRTP, lanRTP, imsRTCP, lanRTCP} {
		if err := setDSCP(conn); err != nil {
			logging.WarnRate("media-dscp:"+packetConnAddrString(conn.LocalAddr()), time.Minute,
				"RTP socket DSCP setup failed", "error", err)
		}
	}
	return relay, nil
}

func resolveBindIP(address string) (net.IP, error) {
	host := strings.TrimSpace(address)
	if splitHost, _, err := net.SplitHostPort(host); err == nil {
		host = splitHost
	}
	host = strings.Trim(host, "[]")
	ip := net.ParseIP(host)
	if ip == nil {
		return nil, fmt.Errorf("media: invalid IMS bind address %q", address)
	}
	return ip, nil
}

// SetRemoteAddr accepts an original host/port pair or an additive UDPAddr.
func (r *RTPRelay) SetRemoteAddr(target any, ports ...int) error {
	addr, err := resolveMediaAddr(target, ports...)
	if err != nil {
		return err
	}
	r.remoteAddr.Store(cloneUDPAddr(addr))
	r.remoteAddrRTCP.Store(offsetUDPAddr(addr, 1))
	r.mu.Lock()
	r.imsRemote = cloneUDPAddr(addr)
	r.mu.Unlock()
	return nil
}

// SetClientAddr accepts an original host/port pair or an additive UDPAddr.
func (r *RTPRelay) SetClientAddr(target any, ports ...int) error {
	addr, err := resolveMediaAddr(target, ports...)
	if err != nil {
		return err
	}
	r.clientAddr.Store(cloneUDPAddr(addr))
	r.clientAddrRTCP.Store(offsetUDPAddr(addr, 1))
	r.mu.Lock()
	r.lanRemote = cloneUDPAddr(addr)
	r.mu.Unlock()
	return nil
}

func resolveMediaAddr(target any, ports ...int) (*net.UDPAddr, error) {
	switch value := target.(type) {
	case *net.UDPAddr:
		if value == nil {
			return nil, errors.New("media: UDP address is nil")
		}
		return value, nil
	case string:
		if len(ports) != 1 || ports[0] <= 0 || ports[0] > 65535 {
			return nil, errors.New("media: host requires a valid UDP port")
		}
		return net.ResolveUDPAddr("udp", net.JoinHostPort(strings.Trim(value, "[]"), fmt.Sprint(ports[0])))
	default:
		return nil, fmt.Errorf("media: unsupported UDP address %T", target)
	}
}

func cloneUDPAddr(addr *net.UDPAddr) *net.UDPAddr {
	if addr == nil {
		return nil
	}
	return &net.UDPAddr{IP: append(net.IP(nil), addr.IP...), Port: addr.Port, Zone: addr.Zone}
}

func offsetUDPAddr(addr *net.UDPAddr, offset int) *net.UDPAddr {
	result := cloneUDPAddr(addr)
	result.Port += offset
	return result
}

// SetPTMapping accepts either one original IMS/LAN pair or an additive map.
func (r *RTPRelay) SetPTMapping(mapping any, rest ...int) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	current := r.ptMap.Load()
	next := clonePTMapping(current)
	switch value := mapping.(type) {
	case int:
		if len(rest) != 1 || value == rest[0] {
			return
		}
		next.imsToLan[value] = rest[0]
		next.lanToIms[rest[0]] = value
	case map[int]int:
		next = &ptMapping{imsToLan: map[int]int{}, lanToIms: map[int]int{}}
		for lanPT, imsPT := range value {
			if lanPT == imsPT {
				continue
			}
			next.imsToLan[imsPT] = lanPT
			next.lanToIms[lanPT] = imsPT
		}
	default:
		return
	}
	r.ptMap.Store(next)
}

func clonePTMapping(source *ptMapping) *ptMapping {
	result := &ptMapping{imsToLan: map[int]int{}, lanToIms: map[int]int{}}
	if source == nil {
		return result
	}
	for key, value := range source.imsToLan {
		result.imsToLan[key] = value
	}
	for key, value := range source.lanToIms {
		result.lanToIms[key] = value
	}
	return result
}

// SetLogContext installs device and trace context.
func (r *RTPRelay) SetLogContext(deviceID string, traceID ...string) {
	if r == nil {
		return
	}
	r.mu.Lock()
	r.deviceID = deviceID
	if len(traceID) > 0 {
		r.traceID = traceID[0]
	}
	r.mu.Unlock()
}

func (r *RTPRelay) logContext() (string, string) {
	if r == nil {
		return "", ""
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.deviceID, r.traceID
}

// IMSPort returns the IMS RTP port.
func (r *RTPRelay) IMSPort() int { return packetConnPort(r.connIMS) }

// LANPort returns the client RTP port.
func (r *RTPRelay) LANPort() int { return packetConnPort(r.lanRTPConn()) }

func packetConnPort(conn net.PacketConn) int {
	addr := packetConnUDPAddr(conn)
	if addr == nil {
		return 0
	}
	return addr.Port
}

func (r *RTPRelay) lanRTPConn() net.PacketConn {
	if r == nil {
		return nil
	}
	if r.connLAN != nil {
		return r.connLAN
	}
	return r.lanPacket
}

// GetIMSConnAndRemote returns the RTP socket and a detached remote address.
func (r *RTPRelay) GetIMSConnAndRemote() (net.PacketConn, *net.UDPAddr) {
	if r == nil {
		return nil, nil
	}
	return r.connIMS, cloneUDPAddr(r.remoteAddr.Load())
}

// Start launches each available RTP and RTCP read loop exactly once.
func (r *RTPRelay) Start() {
	_ = r.StartCurrent()
}

// StartCurrent retains explicit lifecycle error propagation.
func (r *RTPRelay) StartCurrent() error {
	if r == nil {
		return errors.New("media: nil relay")
	}
	r.mu.Lock()
	if r.isStopped() {
		r.mu.Unlock()
		return errors.New("media: relay stopped")
	}
	if r.active {
		r.mu.Unlock()
		return nil
	}
	r.active = true
	r.startLoop(r.connIMS, r.loopIMS)
	r.startLoop(r.lanRTPConn(), r.loopLAN)
	r.startLoop(r.connIMSRTCP, r.loopIMSRTCP)
	if r.connLANRTCP != nil {
		r.startLoop(r.connLANRTCP, r.loopLANRTCP)
	}
	r.startRTCPKeepaliveLoop()
	r.mu.Unlock()
	return nil
}

func (r *RTPRelay) startLoop(conn net.PacketConn, loop func()) {
	if conn == nil {
		return
	}
	r.wg.Add(1)
	go func() {
		defer r.wg.Done()
		loop()
	}()
}

// Stop closes all sockets, stops timers and waits for every relay goroutine.
func (r *RTPRelay) Stop() {
	_ = r.StopCurrent()
}

// StopCurrent retains explicit lifecycle error propagation.
func (r *RTPRelay) StopCurrent() error {
	if r == nil {
		return nil
	}
	r.stopOnce.Do(func() {
		r.mu.Lock()
		close(r.stopCh)
		r.active = false
		monitor := r.Monitor
		r.mu.Unlock()
		r.stopRTCPKeepalive()
		if monitor != nil {
			monitor.stop()
		}
		connections := []net.PacketConn{r.connIMS, r.lanRTPConn(), r.connIMSRTCP}
		if r.connLANRTCP != nil {
			connections = append(connections, r.connLANRTCP)
		}
		stopErr := closePacketConns(connections...)
		r.wg.Wait()
		stopErr = errors.Join(stopErr, r.StopPCAP())
		r.mu.Lock()
		r.stopErr = stopErr
		r.mu.Unlock()
	})
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.stopErr
}

func closePacketConns(conns ...net.PacketConn) error {
	seen := map[net.PacketConn]struct{}{}
	var result error
	for _, conn := range conns {
		if conn == nil {
			continue
		}
		if _, exists := seen[conn]; exists {
			continue
		}
		seen[conn] = struct{}{}
		result = errors.Join(result, conn.Close())
	}
	return result
}

func (r *RTPRelay) shouldStop() bool {
	if r == nil || r.isStopped() {
		return true
	}
	r.mu.RLock()
	active := r.active
	r.mu.RUnlock()
	return !active
}

func (r *RTPRelay) isStopped() bool {
	select {
	case <-r.stopCh:
		return true
	default:
		return false
	}
}

func (r *RTPRelay) loopIMS() {
	r.readLoop(r.connIMS, func(packet []byte, source *net.UDPAddr) {
		r.handleIMSPacket(packet, source)
	})
}

func (r *RTPRelay) loopLAN() {
	r.readLoop(r.lanRTPConn(), func(packet []byte, source *net.UDPAddr) {
		r.handleLANPacket(packet, source)
	})
}

func (r *RTPRelay) readLoop(conn net.PacketConn, handle func([]byte, *net.UDPAddr)) {
	buffer := make([]byte, mediaReadBufferSize)
	for !r.shouldStop() {
		n, source, err := conn.ReadFrom(buffer)
		if err != nil {
			if isRTPRelayReadClosedError(err) || r.isStopped() {
				return
			}
			if !isRTPRelayReadTimeout(err) {
				r.logReadError(err)
			}
			continue
		}
		udpSource := packetConnAddrToUDPAddr(source)
		if udpSource == nil {
			deviceID, _ := r.logContext()
			logging.WarnRate("media-source:"+deviceID, time.Minute,
				"RTP source address is not UDP", "address", packetConnAddrString(source))
			continue
		}
		packet := append([]byte(nil), buffer[:n]...)
		handle(packet, udpSource)
	}
}

func (r *RTPRelay) logReadError(err error) {
	deviceID, traceID := r.logContext()
	logging.WarnRate("media-read:"+deviceID, time.Second,
		"RTP relay read failed", "device", deviceID, "trace", traceID, "error", err)
}

func isRTPRelayReadClosedError(err error) bool {
	return errors.Is(err, net.ErrClosed) || strings.Contains(strings.ToLower(errString(err)), "closed network connection")
}

func isRTPRelayReadTimeout(err error) bool {
	var netErr net.Error
	return errors.As(err, &netErr) && netErr.Timeout()
}

func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

// Stats returns the byte counters for RTP and RTCP in both directions.
func (r *RTPRelay) Stats() (uint64, uint64, uint64, uint64) {
	if r == nil {
		return 0, 0, 0, 0
	}
	return atomic.LoadUint64(&r.bytesIMSToLAN), atomic.LoadUint64(&r.bytesLANToIMS),
		atomic.LoadUint64(&r.bytesIMSRTCPToLAN), atomic.LoadUint64(&r.bytesLANRTCPToIMS)
}
