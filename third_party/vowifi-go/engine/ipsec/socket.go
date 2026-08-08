package ipsec

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
)

const (
	ikeQueueSize      = 100
	espQueueSize      = 1000
	networkQueueSize  = 10
	directReadBufSize = 4096
)

// SocketStats is a snapshot of direct-transport receive counters.
type SocketStats struct {
	ReceivedIKE uint64
	ReceivedESP uint64
	DroppedIKE  uint64
	DroppedESP  uint64
}

// Stats retains the reconstructed type name as an alias.
type Stats = SocketStats

// SocketManager exchanges IKE and ESP datagrams directly with the ePDG.
type SocketManager struct {
	receivedIKE uint64
	receivedESP uint64
	droppedIKE  uint64
	droppedESP  uint64

	DeviceID   string
	Conn       *net.UDPConn
	LocalAddr  *net.UDPAddr
	RemoteAddr *net.UDPAddr
	remoteIPs  []net.IP
	remoteMu   sync.Mutex
	remoteIdx  uint32

	IKEChan   chan []byte
	ESPChan   chan []byte
	NetEvents chan NetEvent
	closeChan chan struct{}
	wg        sync.WaitGroup
	lifecycle sync.Mutex
	started   bool
	stopped   bool
	startErr  error
}

// NewSocketManager resolves remote, binds local, and prepares a transport.
func NewSocketManager(deviceID, local, remote, dnsServer string) (*SocketManager, error) {
	remoteAddr, remoteIPs, err := ResolveUDPAddrAll(remote, dnsServer)
	if err != nil {
		return nil, fmt.Errorf("resolve remote address %q: %w", remote, err)
	}
	network := "udp6"
	if remoteAddr.IP.To4() != nil {
		network = "udp4"
	}
	if strings.TrimSpace(local) == "" {
		local = ":0"
	}
	localAddr, err := net.ResolveUDPAddr(network, local)
	if err != nil {
		return nil, fmt.Errorf("resolve local address %q: %w", local, err)
	}
	listenConfig := net.ListenConfig{Control: reuseSocketOptions}
	packetConn, err := listenConfig.ListenPacket(
		context.Background(), network, localAddr.String(),
	)
	if err != nil {
		return nil, fmt.Errorf("listen on %s: %w", localAddr, err)
	}
	conn, ok := packetConn.(*net.UDPConn)
	if !ok {
		_ = packetConn.Close()
		return nil, errors.New("underlying connection is not UDP")
	}
	actual, ok := conn.LocalAddr().(*net.UDPAddr)
	if !ok {
		_ = conn.Close()
		return nil, errors.New("UDP socket has no local UDP address")
	}
	return &SocketManager{
		DeviceID: deviceID, Conn: conn, LocalAddr: actual, RemoteAddr: remoteAddr,
		remoteIPs: remoteIPs, IKEChan: make(chan []byte, ikeQueueSize),
		ESPChan: make(chan []byte, espQueueSize), NetEvents: make(chan NetEvent, networkQueueSize),
		closeChan: make(chan struct{}),
	}, nil
}

func reuseSocketOptions(_, _ string, raw syscall.RawConn) error {
	var optionErr error
	err := raw.Control(func(fd uintptr) {
		optionErr = syscall.SetsockoptInt(int(fd), syscall.SOL_SOCKET, syscall.SO_REUSEADDR, 1)
		if optionErr == nil {
			optionErr = syscall.SetsockoptInt(int(fd), syscall.SOL_SOCKET, soReusePort, 1)
		}
	})
	if err != nil {
		return err
	}
	return optionErr
}

func (s *SocketManager) IKEPackets() <-chan []byte      { return s.IKEChan }
func (s *SocketManager) ESPPackets() <-chan []byte      { return s.ESPChan }
func (s *SocketManager) NetEventsChan() <-chan NetEvent { return s.NetEvents }

func (s *SocketManager) ReceiveIKE() ([]byte, error) {
	packet, ok := <-s.IKEChan
	if !ok {
		return nil, errors.New("IKE channel closed")
	}
	return packet, nil
}

func (s *SocketManager) LocalIP() net.IP {
	if s.LocalAddr == nil {
		return nil
	}
	return s.LocalAddr.IP
}

func (s *SocketManager) LocalPort() uint16 {
	if s.LocalAddr == nil {
		return 0
	}
	return uint16(s.LocalAddr.Port)
}

func (s *SocketManager) RemoteIP() net.IP {
	s.remoteMu.Lock()
	defer s.remoteMu.Unlock()
	if s.RemoteAddr == nil {
		return nil
	}
	return append(net.IP(nil), s.RemoteAddr.IP...)
}

func (s *SocketManager) RemotePort() int {
	s.remoteMu.Lock()
	defer s.remoteMu.Unlock()
	if s.RemoteAddr == nil {
		return 0
	}
	return s.RemoteAddr.Port
}

func (s *SocketManager) SetRemotePort(port int) {
	s.remoteMu.Lock()
	if s.RemoteAddr != nil {
		s.RemoteAddr.Port = port
	}
	s.remoteMu.Unlock()
}

func (s *SocketManager) LocalAddrString() string {
	if s.LocalAddr == nil {
		return ""
	}
	return s.LocalAddr.String()
}

func (s *SocketManager) RemoteAddrString() string {
	s.remoteMu.Lock()
	defer s.remoteMu.Unlock()
	if s.RemoteAddr == nil {
		return ""
	}
	return s.RemoteAddr.String()
}

func (s *SocketManager) Stats() SocketStats {
	return SocketStats{
		ReceivedIKE: atomic.LoadUint64(&s.receivedIKE),
		ReceivedESP: atomic.LoadUint64(&s.receivedESP),
		DroppedIKE:  atomic.LoadUint64(&s.droppedIKE),
		DroppedESP:  atomic.LoadUint64(&s.droppedESP),
	}
}

func (s *SocketManager) Start() error {
	s.lifecycle.Lock()
	defer s.lifecycle.Unlock()
	if s.stopped {
		return errors.New("socket transport already stopped")
	}
	if s.started {
		return s.startErr
	}
	if s.Conn == nil {
		return errors.New("socket not created")
	}
	s.started = true
	s.wg.Add(2)
	go s.readLoop()
	go s.startErrorListener()
	return s.startErr
}

func (s *SocketManager) Stop() {
	s.lifecycle.Lock()
	if s.stopped {
		s.lifecycle.Unlock()
		return
	}
	s.stopped = true
	close(s.closeChan)
	if s.Conn != nil {
		_ = s.Conn.Close()
	}
	s.lifecycle.Unlock()
	s.wg.Wait()
	close(s.IKEChan)
	close(s.ESPChan)
	close(s.NetEvents)
}

func (s *SocketManager) RawFD() (int, error) {
	if s.Conn == nil {
		return -1, errors.New("socket not created")
	}
	raw, err := s.Conn.SyscallConn()
	if err != nil {
		return -1, fmt.Errorf("get UDP syscall connection: %w", err)
	}
	fd := -1
	if err := raw.Control(func(value uintptr) { fd = int(value) }); err != nil {
		return -1, fmt.Errorf("get UDP file descriptor: %w", err)
	}
	return fd, nil
}

// SetUDPEncap enables the kernel's UDP_ENCAP_ESPINUDP processing.
func (s *SocketManager) SetUDPEncap() error { return s.setUDPEncap(true) }

// DisableUDPEncap reverses SetUDPEncap for transactional XFRM cleanup.
func (s *SocketManager) DisableUDPEncap() error { return s.setUDPEncap(false) }

func (s *SocketManager) setUDPEncap(enable bool) error {
	if s.Conn == nil {
		return errors.New("socket not created")
	}
	if err := setUDPEncap(s.Conn, enable); err != nil {
		return err
	}
	logInfo(fmt.Sprintf("UDP encapsulation %t, local addr %s", enable, s.LocalAddrString()))
	return nil
}
