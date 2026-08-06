package ipsec

import (
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
)

// SOCKS5 constants (RFC 1928).
const (
	socks5Version        = 5
	socks5CmdConnect     = 1
	socks5CmdUDPAssociate = 3
	socks5MethodNoAuth   = 0
	socks5MethodUserPass = 2
	socks5NoAcceptable   = 0xff
	socks5ATYPIPv4       = 1
	socks5ATYPDomain     = 3
	socks5ATYPIPv6       = 4
	socks5DefaultPort    = 1080
)

// socks5Handshake negotiates the SOCKS5 version and authentication method
// (RFC 1928 §3). If config has credentials it also offers username/password
// authentication (RFC 1929).
func socks5Handshake(conn io.ReadWriter, config *Socks5Config) error {
	methods := []byte{socks5MethodNoAuth}
	if config != nil && len(config.Username) > 0 {
		methods = []byte{socks5MethodNoAuth, socks5MethodUserPass}
	}
	greeting := append([]byte{socks5Version, byte(len(methods))}, methods...)
	if _, err := conn.Write(greeting); err != nil {
		return fmt.Errorf("write greeting: %w", err)
	}
	resp := make([]byte, 2)
	if _, err := io.ReadFull(conn, resp); err != nil {
		return fmt.Errorf("read greeting reply: %w", err)
	}
	if resp[0] != socks5Version {
		return fmt.Errorf("unsupported SOCKS version %d", resp[0])
	}
	switch resp[1] {
	case socks5MethodNoAuth:
		return nil
	case socks5MethodUserPass:
		if config != nil && len(config.Username) > 0 {
			return socks5UserPasswordAuth(conn, config)
		}
		return errors.New("username/password required but no credentials configured")
	case socks5NoAcceptable:
		return errors.New("no acceptable authentication methods")
	default:
		return fmt.Errorf("unsupported authentication method %d", resp[1])
	}
}

// socks5UserPasswordAuth performs RFC 1929 authentication.
func socks5UserPasswordAuth(conn io.ReadWriter, config *Socks5Config) error {
	u, p := config.Username, config.Password
	if len(u) > 255 || len(p) > 255 {
		return errors.New("credentials too long")
	}
	req := []byte{1, byte(len(u))}
	req = append(req, u...)
	req = append(req, byte(len(p)))
	req = append(req, p...)
	if _, err := conn.Write(req); err != nil {
		return fmt.Errorf("write auth request: %w", err)
	}
	resp := make([]byte, 2)
	if _, err := io.ReadFull(conn, resp); err != nil {
		return fmt.Errorf("read auth reply: %w", err)
	}
	if resp[1] != 0 {
		return errors.New("authentication failed")
	}
	return nil
}

// socks5UDPAssociate requests a UDP associate session (RFC 1928 §7) and
// returns the relay address to send UDP datagrams to. target is the address
// announced to the proxy (the local ePDG endpoint); a nil target uses
// 0.0.0.0:0.
func socks5UDPAssociate(conn io.ReadWriter, config *Socks5Config, target *net.UDPAddr) (*net.UDPAddr, error) {
	if target == nil {
		target = &net.UDPAddr{IP: net.IPv4zero, Port: 0}
	}
	req := buildSocks5Request(socks5CmdUDPAssociate, target.IP, uint16(target.Port))
	if _, err := conn.Write(req); err != nil {
		return nil, fmt.Errorf("write UDP associate request: %w", err)
	}
	relay, err := readSocks5Reply(conn)
	if err != nil {
		return nil, fmt.Errorf("UDP associate reply: %w", err)
	}
	return relay, nil
}

// buildSocks5Request builds a SOCKS5 request header (RFC 1928 §4). IPv4
// mapped IPv6 addresses are converted to plain IPv4; an unrecognised address
// falls back to 0.0.0.0.
func buildSocks5Request(cmd byte, ip net.IP, port uint16) []byte {
	ip = ipv4Compat(ip)
	if v4 := ip.To4(); v4 != nil {
		req := []byte{socks5Version, cmd, 0, socks5ATYPIPv4}
		req = append(req, v4...)
		return append(req, byte(port>>8), byte(port))
	}
	if v6 := ip.To16(); v6 != nil {
		req := []byte{socks5Version, cmd, 0, socks5ATYPIPv6}
		req = append(req, v6...)
		return append(req, byte(port>>8), byte(port))
	}
	req := []byte{socks5Version, cmd, 0, socks5ATYPIPv4, 0, 0, 0, 0}
	return append(req, byte(port>>8), byte(port))
}

// readSocks5Reply reads and validates a SOCKS5 reply (RFC 1928 §6), returning
// the bound address/port.
func readSocks5Reply(conn io.Reader) (*net.UDPAddr, error) {
	hdr := make([]byte, 4)
	if _, err := io.ReadFull(conn, hdr); err != nil {
		return nil, fmt.Errorf("read reply header: %w", err)
	}
	if hdr[0] != socks5Version {
		return nil, fmt.Errorf("unsupported SOCKS version %d", hdr[0])
	}
	if hdr[1] != 0 {
		return nil, fmt.Errorf("SOCKS5 connection failed: %s", socks5ReplyString(hdr[1]))
	}
	var ip net.IP
	switch hdr[3] {
	case socks5ATYPIPv4:
		b := make([]byte, 4)
		if _, err := io.ReadFull(conn, b); err != nil {
			return nil, fmt.Errorf("read IPv4 address: %w", err)
		}
		ip = net.IPv4(b[0], b[1], b[2], b[3])
	case socks5ATYPDomain:
		ln := make([]byte, 1)
		if _, err := io.ReadFull(conn, ln); err != nil {
			return nil, fmt.Errorf("read domain length: %w", err)
		}
		host := make([]byte, int(ln[0]))
		if _, err := io.ReadFull(conn, host); err != nil {
			return nil, fmt.Errorf("read domain name: %w", err)
		}
		resolved, err := net.ResolveIPAddr("ip", string(host))
		if err != nil {
			return nil, fmt.Errorf("resolve %q: %w", host, err)
		}
		ip = resolved.IP
	case socks5ATYPIPv6:
		b := make([]byte, 16)
		if _, err := io.ReadFull(conn, b); err != nil {
			return nil, fmt.Errorf("read IPv6 address: %w", err)
		}
		ip = net.IP(append([]byte{}, b...))
	default:
		return nil, fmt.Errorf("unsupported address type %d", hdr[3])
	}
	portB := make([]byte, 2)
	if _, err := io.ReadFull(conn, portB); err != nil {
		return nil, fmt.Errorf("read port: %w", err)
	}
	return &net.UDPAddr{IP: ip, Port: int(portB[0])<<8 | int(portB[1])}, nil
}

// socks5ReplyString maps a SOCKS5 reply code to its RFC 1928 description.
func socks5ReplyString(rep byte) string {
	switch rep {
	case 0:
		return "succeeded"
	case 1:
		return "general failure"
	case 2:
		return "connection not allowed by ruleset"
	case 3:
		return "network unreachable"
	case 4:
		return "host unreachable"
	case 5:
		return "connection refused"
	case 6:
		return "TTL expired"
	case 7:
		return "command not supported"
	case 8:
		return "address type not supported"
	default:
		return fmt.Sprintf("unknown error %d", rep)
	}
}

// Socks5UDPDatagram is a decoded SOCKS5 UDP datagram (RFC 1928 §7).
type Socks5UDPDatagram struct {
	Frag byte
	Addr *net.UDPAddr
	Data []byte
}

// EncodeSocks5UDPDatagram wraps data in a SOCKS5 UDP relay datagram for the
// given destination address.
func EncodeSocks5UDPDatagram(addr *net.UDPAddr, data []byte) []byte {
	ip := addr.IP
	if ip == nil {
		ip = net.IPv4zero
	}
	ip = ipv4Compat(ip)
	if v4 := ip.To4(); v4 != nil {
		dgram := make([]byte, 0, 10+len(data))
		dgram = append(dgram, 0, 0, 0, socks5ATYPIPv4) // RSV(2) | FRAG(0) | ATYP
		dgram = append(dgram, v4...)
		dgram = append(dgram, byte(addr.Port>>8), byte(addr.Port))
		return append(dgram, data...)
	}
	v6 := ip.To16()
	dgram := make([]byte, 0, 22+len(data))
	dgram = append(dgram, 0, 0, 0, socks5ATYPIPv6)
	dgram = append(dgram, v6...)
	dgram = append(dgram, byte(addr.Port>>8), byte(addr.Port))
	return append(dgram, data...)
}

// DecodeSocks5UDPDatagram parses a SOCKS5 UDP relay datagram.
func DecodeSocks5UDPDatagram(dgram []byte) (*Socks5UDPDatagram, error) {
	if len(dgram) < 4 {
		return nil, errors.New("datagram too short")
	}
	frag := dgram[2]
	var ip net.IP
	var hdrLen int
	switch dgram[3] {
	case socks5ATYPIPv4:
		if len(dgram) < 10 {
			return nil, errors.New("datagram too short for IPv4 address")
		}
		ip = net.IPv4(dgram[4], dgram[5], dgram[6], dgram[7])
		hdrLen = 10
	case socks5ATYPDomain:
		if len(dgram) < 5 {
			return nil, errors.New("datagram too short for domain name")
		}
		n := int(dgram[4])
		if len(dgram) < 5+n+2 {
			return nil, errors.New("datagram too short for domain name")
		}
		host := string(dgram[5 : 5+n])
		resolved, err := net.ResolveIPAddr("ip", host)
		if err != nil {
			return nil, fmt.Errorf("resolve %q: %w", host, err)
		}
		ip = resolved.IP
		hdrLen = 5 + n + 2
	case socks5ATYPIPv6:
		if len(dgram) < 22 {
			return nil, errors.New("datagram too short for IPv6 address")
		}
		ip = net.IP(append([]byte{}, dgram[4:20]...))
		hdrLen = 22
	default:
		return nil, fmt.Errorf("unsupported address type %d", dgram[3])
	}
	port := int(dgram[hdrLen-2])<<8 | int(dgram[hdrLen-1])
	return &Socks5UDPDatagram{
		Frag: frag,
		Addr: &net.UDPAddr{IP: ip, Port: port},
		Data: dgram[hdrLen:],
	}, nil
}

// parseSocks5Addr splits "host:port" (or a bare host, defaulting to port
// 1080) into its parts.
func parseSocks5Addr(addr string) (string, int, error) {
	if host, portStr, err := net.SplitHostPort(addr); err == nil {
		p, err := strconv.Atoi(portStr)
		if err != nil || p < 1 || p > 0xffff {
			return "", 0, fmt.Errorf("invalid port %q", portStr)
		}
		return host, p, nil
	}
	if strings.Contains(addr, ":") {
		return "", 0, errors.New("invalid socks5 address")
	}
	return addr, socks5DefaultPort, nil
}
