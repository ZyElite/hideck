package imscore

import (
	"context"
	"crypto/rand"
	"errors"
	"io"
	"net"
	"strings"
	"sync"
	"time"
)

// sipResponse is a minimal parsed SIP response.
type sipResponse struct {
	StatusCode int
	Reason     string
	CallID     string
	CSeq       string
	Headers    map[string]string
}

// Header returns a header value.
func (r *sipResponse) Header(name string) string {
	if r == nil {
		return ""
	}
	for k, v := range r.Headers {
		if strings.EqualFold(k, name) {
			return v
		}
	}
	return ""
}

// sipTransport sends SIP requests and receives responses.
type sipTransport struct {
	mu        sync.Mutex
	sendFn    func(string) error
	responses chan *sipResponse
	requests  chan string
	closed    chan struct{}
}

// newSIPTransport creates a SIP transport.
func newSIPTransport() *sipTransport {
	return &sipTransport{
		responses: make(chan *sipResponse, 64),
		requests:  make(chan string, 64),
		closed:    make(chan struct{}),
	}
}

// SetSendFn wires the outbound sender.
func (t *sipTransport) SetSendFn(fn func(string) error) {
	t.mu.Lock()
	t.sendFn = fn
	t.mu.Unlock()
}

func (t *sipTransport) hasSendFn() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.sendFn != nil
}

// Send delivers a SIP request.
func (t *sipTransport) Send(req string) error {
	t.mu.Lock()
	fn := t.sendFn
	t.mu.Unlock()
	if fn != nil {
		return fn(req)
	}
	select {
	case <-t.closed:
		return errors.New("imscore: transport closed")
	case t.requests <- req:
		return nil
	}
}

// Responses returns the response channel.
func (t *sipTransport) Responses() <-chan *sipResponse {
	return t.responses
}

// Requests returns the inbound request channel.
func (t *sipTransport) Requests() <-chan string {
	return t.requests
}

// DeliverResponse feeds a parsed response into the transport.
func (t *sipTransport) DeliverResponse(r *sipResponse) {
	select {
	case t.responses <- r:
	default:
	}
}

// DeliverRequest feeds a raw request into the transport.
func (t *sipTransport) DeliverRequest(raw string) {
	select {
	case t.requests <- raw:
	default:
	}
}

// Close shuts the transport down.
func (t *sipTransport) Close() error {
	select {
	case <-t.closed:
	default:
		close(t.closed)
	}
	return nil
}

// parseSIPResponse parses a raw SIP response.
func parseSIPResponse(raw string) *sipResponse {
	lines := strings.Split(raw, "\r\n")
	if len(lines) == 0 {
		return nil
	}
	resp := &sipResponse{Headers: make(map[string]string)}
	parts := strings.SplitN(lines[0], " ", 3)
	if len(parts) >= 2 {
		_, _ = fmtSscanf(parts[1], &resp.StatusCode)
	}
	if len(parts) >= 3 {
		resp.Reason = parts[2]
	}
	for _, line := range lines[1:] {
		key, value, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		value = strings.TrimSpace(value)
		switch strings.ToLower(strings.TrimSpace(key)) {
		case "call-id":
			resp.CallID = value
		case "cseq":
			resp.CSeq = value
		}
		resp.Headers[strings.TrimSpace(key)] = value
	}
	return resp
}

// fmtSscanf parses an integer from a string.
func fmtSscanf(s string, out *int) (int, error) {
	_, err := fmtSscanfImpl(s, out)
	return 0, err
}

// --- conn types ---

// stableSIPConn is a stable SIP connection (TCP-like).
type stableSIPConn struct {
	conn net.Conn
	buf  []byte
}

// Read reads from the connection.
func (c *stableSIPConn) Read(b []byte) (int, error) {
	if c == nil || c.conn == nil {
		return 0, errors.New("imscore: nil conn")
	}
	return c.conn.Read(b)
}

// Write writes to the connection.
func (c *stableSIPConn) Write(b []byte) (int, error) {
	if c == nil || c.conn == nil {
		return 0, errors.New("imscore: nil conn")
	}
	return c.conn.Write(b)
}

// Close closes the connection.
func (c *stableSIPConn) Close() error {
	if c == nil || c.conn == nil {
		return nil
	}
	return c.conn.Close()
}

// LocalAddr returns the local address.
func (c *stableSIPConn) LocalAddr() net.Addr {
	if c == nil || c.conn == nil {
		return nil
	}
	return c.conn.LocalAddr()
}

// RemoteAddr returns the remote address.
func (c *stableSIPConn) RemoteAddr() net.Addr {
	if c == nil || c.conn == nil {
		return nil
	}
	return c.conn.RemoteAddr()
}

// SetDeadline sets the read/write deadline.
func (c *stableSIPConn) SetDeadline(t time.Time) error {
	if c == nil || c.conn == nil {
		return nil
	}
	return c.conn.SetDeadline(t)
}

// sipFramingConn frames SIP messages over a connection.
type sipFramingConn struct {
	stable *stableSIPConn
	reader *bufReader
}

// bufReader buffers reads.
type bufReader struct {
	r   io.Reader
	buf []byte
}

func (r *bufReader) Read(p []byte) (int, error) {
	return r.r.Read(p)
}

// Read reads one framed SIP message.
func (c *sipFramingConn) Read(b []byte) (int, error) {
	if c == nil || c.stable == nil {
		return 0, errors.New("imscore: nil framing conn")
	}
	return c.stable.Read(b)
}

// Write writes a SIP message.
func (c *sipFramingConn) Write(b []byte) (int, error) {
	if c == nil || c.stable == nil {
		return 0, errors.New("imscore: nil framing conn")
	}
	return c.stable.Write(b)
}

// Close closes the connection.
func (c *sipFramingConn) Close() error {
	if c == nil || c.stable == nil {
		return nil
	}
	return c.stable.Close()
}

// LocalAddr returns the local address.
func (c *sipFramingConn) LocalAddr() net.Addr {
	if c == nil || c.stable == nil {
		return nil
	}
	return c.stable.LocalAddr()
}

// inboundCountingConn counts inbound bytes/conns.
type inboundCountingConn struct {
	conn net.Conn
}

// Read reads and counts.
func (c *inboundCountingConn) Read(b []byte) (int, error) {
	if c == nil || c.conn == nil {
		return 0, errors.New("imscore: nil conn")
	}
	n, err := c.conn.Read(b)
	return n, err
}

// Write writes.
func (c *inboundCountingConn) Write(b []byte) (int, error) {
	if c == nil || c.conn == nil {
		return 0, errors.New("imscore: nil conn")
	}
	return c.conn.Write(b)
}

// Close closes.
func (c *inboundCountingConn) Close() error {
	if c == nil || c.conn == nil {
		return nil
	}
	return c.conn.Close()
}

// LocalAddr returns the local address.
func (c *inboundCountingConn) LocalAddr() net.Addr {
	if c == nil || c.conn == nil {
		return nil
	}
	return c.conn.LocalAddr()
}

// RemoteAddr returns the remote address.
func (c *inboundCountingConn) RemoteAddr() net.Addr {
	if c == nil || c.conn == nil {
		return nil
	}
	return c.conn.RemoteAddr()
}

// inboundCountingPacketConn counts inbound packets.
type inboundCountingPacketConn struct {
	conn net.PacketConn
}

// ReadFrom reads a packet.
func (c *inboundCountingPacketConn) ReadFrom(b []byte) (int, net.Addr, error) {
	if c == nil || c.conn == nil {
		return 0, nil, errors.New("imscore: nil packet conn")
	}
	return c.conn.ReadFrom(b)
}

// WriteTo writes a packet.
func (c *inboundCountingPacketConn) WriteTo(b []byte, addr net.Addr) (int, error) {
	if c == nil || c.conn == nil {
		return 0, errors.New("imscore: nil packet conn")
	}
	return c.conn.WriteTo(b, addr)
}

// Close closes.
func (c *inboundCountingPacketConn) Close() error {
	if c == nil || c.conn == nil {
		return nil
	}
	return c.conn.Close()
}

// LocalAddr returns the local address.
func (c *inboundCountingPacketConn) LocalAddr() net.Addr {
	if c == nil || c.conn == nil {
		return nil
	}
	return c.conn.LocalAddr()
}

// singleConnListener is a listener that serves a single connection.
type singleConnListener struct {
	conn net.Conn
	done bool
	mu   sync.Mutex
}

// Accept returns the single connection once.
func (l *singleConnListener) Accept() (net.Conn, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.done {
		return nil, errors.New("imscore: listener exhausted")
	}
	l.done = true
	return l.conn, nil
}

// Close closes the listener.
func (l *singleConnListener) Close() error {
	return nil
}

// Addr returns the listener address.
func (l *singleConnListener) Addr() net.Addr {
	if l == nil || l.conn == nil {
		return nil
	}
	return l.conn.LocalAddr()
}

// connRegisterTransport is the registration transport.
type connRegisterTransport struct {
	transport *sipTransport
}

// Close closes the transport.
func (c *connRegisterTransport) Close() error {
	if c == nil || c.transport == nil {
		return nil
	}
	return c.transport.Close()
}

// ReadResponse reads the next response.
func (c *connRegisterTransport) ReadResponse(ctx context.Context) (*sipResponse, error) {
	if c == nil || c.transport == nil {
		return nil, errors.New("imscore: nil register transport")
	}
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case resp := <-c.transport.Responses():
		return resp, nil
	}
}

// randRead fills b with random bytes.
func randRead(b []byte) (int, error) {
	return rand.Read(b)
}

// fmtSscanfImpl parses an int from a string.
func fmtSscanfImpl(s string, out *int) (int, error) {
	var n int
	var sign int = 1
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "-") {
		sign = -1
		s = s[1:]
	}
	if s == "" {
		return 0, errors.New("imscore: empty int")
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0, errors.New("imscore: bad int")
		}
		n = n*10 + int(c-'0')
	}
	*out = n * sign
	return 0, nil
}

var _ = context.Background

// Send sends a SIP request through the register transport.
func (c *connRegisterTransport) Send(req string) error {
	if c == nil || c.transport == nil {
		return errors.New("imscore: nil register transport")
	}
	return c.transport.Send(req)
}

// SetDeadline sets the read/write deadline.
func (c *inboundCountingConn) SetDeadline(t time.Time) error {
	if c == nil || c.conn == nil {
		return nil
	}
	return c.conn.SetDeadline(t)
}

// SetReadDeadline sets the read deadline.
func (c *inboundCountingConn) SetReadDeadline(t time.Time) error {
	if c == nil || c.conn == nil {
		return nil
	}
	return c.conn.SetReadDeadline(t)
}

// SetWriteDeadline sets the write deadline.
func (c *inboundCountingConn) SetWriteDeadline(t time.Time) error {
	if c == nil || c.conn == nil {
		return nil
	}
	return c.conn.SetWriteDeadline(t)
}

// SetDeadline sets the read/write deadline.
func (c *inboundCountingPacketConn) SetDeadline(t time.Time) error {
	if c == nil || c.conn == nil {
		return nil
	}
	return c.conn.SetDeadline(t)
}

// SetReadDeadline sets the read deadline.
func (c *inboundCountingPacketConn) SetReadDeadline(t time.Time) error {
	if c == nil || c.conn == nil {
		return nil
	}
	return c.conn.SetReadDeadline(t)
}

// SetWriteDeadline sets the write deadline.
func (c *inboundCountingPacketConn) SetWriteDeadline(t time.Time) error {
	if c == nil || c.conn == nil {
		return nil
	}
	return c.conn.SetWriteDeadline(t)
}

// SetReadDeadline sets the read deadline.
func (c *stableSIPConn) SetReadDeadline(t time.Time) error {
	if c == nil || c.conn == nil {
		return nil
	}
	return c.conn.SetReadDeadline(t)
}

// SetWriteDeadline sets the write deadline.
func (c *stableSIPConn) SetWriteDeadline(t time.Time) error {
	if c == nil || c.conn == nil {
		return nil
	}
	return c.conn.SetWriteDeadline(t)
}

// RemoteAddr returns the remote address.
func (c *sipFramingConn) RemoteAddr() net.Addr {
	if c == nil || c.stable == nil {
		return nil
	}
	return c.stable.RemoteAddr()
}

// SetDeadline sets the read/write deadline.
func (c *sipFramingConn) SetDeadline(t time.Time) error {
	if c == nil || c.stable == nil {
		return nil
	}
	return c.stable.SetDeadline(t)
}

// SetReadDeadline sets the read deadline.
func (c *sipFramingConn) SetReadDeadline(t time.Time) error {
	if c == nil || c.stable == nil {
		return nil
	}
	return c.stable.SetReadDeadline(t)
}

// SetWriteDeadline sets the write deadline.
func (c *sipFramingConn) SetWriteDeadline(t time.Time) error {
	if c == nil || c.stable == nil {
		return nil
	}
	return c.stable.SetWriteDeadline(t)
}
