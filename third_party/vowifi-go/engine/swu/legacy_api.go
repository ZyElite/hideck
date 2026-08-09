package swu

import (
	"encoding/binary"
	"net"
	"time"

	"github.com/iniwex5/vowifi-go/engine/ikev2"
)

const progressEstablishingIPSec = "正在建立 IPsec 加密隧道..."

// SessionSnapshot is the original structured session status API.
type SessionSnapshot struct {
	Established              bool
	TUNName                  string
	LastError                string
	IKEProfile               string
	IKEEncr                  string
	IKEInteg                 string
	IKEPRF                   string
	IKEDH                    string
	IPv4                     net.IP
	IPv6                     net.IP
	IPv6Prefix               int
	MTU                      uint32
	DNSv4                    []net.IP
	DNSv6                    []net.IP
	PCSCFv4                  []net.IP
	PCSCFv6                  []net.IP
	OfferedIKEProfiles       []string
	EffectiveCipherPolicy    string
	NegotiationFallbackCount int
}

// Snapshot returns a detached copy of the original structured status.
func (s *Session) Snapshot() SessionSnapshot {
	if s == nil {
		return SessionSnapshot{}
	}
	s.mu.RLock()
	out := s.snapshotLocked()
	s.mu.RUnlock()
	s.childSAMu.RLock()
	out.Established = s.espInboundSA != nil && s.espOutboundSA != nil
	s.childSAMu.RUnlock()
	out.TUNName = s.activeDriverInterface()
	mode, err := configuredDataplaneMode(s.cfg)
	if err != nil || mode != DataplaneModeUserspace && out.TUNName == "" {
		out.Established = false
	}
	return out
}

func (s *Session) snapshotLocked() SessionSnapshot {
	out := SessionSnapshot{
		IKEProfile:               snapshotIKEProfile(s.encrAlg, s.integAlg, s.prfAlg),
		IKEEncr:                  ikev2.EncrToString(s.encrAlg),
		IKEInteg:                 ikev2.IntegToString(s.integAlg),
		IKEPRF:                   ikev2.PRFToString(s.prfAlg),
		OfferedIKEProfiles:       append([]string(nil), s.offeredIKEProfiles...),
		EffectiveCipherPolicy:    s.effectiveCipherPolicy,
		NegotiationFallbackCount: s.negotiationFallbackCount,
		IPv4:                     append(net.IP(nil), s.innerIP...), IPv6: append(net.IP(nil), s.innerIPv6...),
		IPv6Prefix: s.innerIPv6Prefix, MTU: uint32(s.tunnelMTU()),
	}
	if s.terminalErr != nil {
		out.LastError = s.terminalErr.Error()
	}
	if s.dh != nil {
		out.IKEDH = ikev2.DHToString(uint16(s.dh.Group))
	}
	out.DNSv4, out.DNSv6 = splitIPFamilies(s.dnsServers)
	out.PCSCFv4, out.PCSCFv6 = splitIPFamilies(s.pcscfServers)
	if out.IPv6 != nil && out.IPv6Prefix == 0 {
		out.IPv6Prefix = 64
	}
	return out
}

// SnapshotMap retains the additive map status API introduced by this branch.
func (s *Session) SnapshotMap() map[string]interface{} {
	if s == nil {
		return nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return map[string]interface{}{
		"state": s.state, "epdg": configuredEPDGAddress(s.cfg),
		"remote_ip": s.remoteIP.String(), "remote_pt": s.remotePort,
		"inner_ip": s.primaryInnerIP().String(), "started_at": s.startedAt,
	}
}

func splitIPFamilies(addresses []net.IP) (ipv4, ipv6 []net.IP) {
	for _, address := range addresses {
		copyIP := append(net.IP(nil), address...)
		if address.To4() != nil {
			ipv4 = append(ipv4, copyIP)
		} else {
			ipv6 = append(ipv6, copyIP)
		}
	}
	return ipv4, ipv6
}

func snapshotIKEProfile(encryption, integrity, prf uint16) string {
	switch {
	case integrity == uint16(ikev2.AUTH_HMAC_SHA2_256_128) && prf == uint16(ikev2.PRF_HMAC_SHA2_256):
		return "sha2_modern"
	case integrity == uint16(ikev2.AUTH_HMAC_SHA1_96) && prf == uint16(ikev2.PRF_HMAC_SHA1):
		return "sha1_legacy"
	case integrity == uint16(ikev2.AUTH_AES_XCBC_96) && prf == uint16(ikev2.PRF_AES128_XCBC):
		return "xcbc_legacy"
	default:
		return "mixed"
	}
}

func (s *Session) syncLegacyIKEStateLocked() {
	s.SPIi = binary.BigEndian.Uint64(s.spiI[:])
	s.SPIr = binary.BigEndian.Uint64(s.spiR[:])
	s.DH = s.dh
	s.SequenceNumber.Store(s.nextOutboundID)
	if s.ikeKeys == nil {
		s.Keys = nil
		return
	}
	s.Keys = &ikev2.IKESAKeys{
		SK_d: s.ikeKeys.SK_d, SK_ai: s.ikeKeys.SK_ai, SK_ar: s.ikeKeys.SK_ar,
		SK_ei: s.ikeKeys.SK_ei, SK_er: s.ikeKeys.SK_er,
		SK_pi: s.ikeKeys.SK_pi, SK_pr: s.ikeKeys.SK_pr,
	}
}

func (s *Session) syncLegacyChildStateLocked() {
	s.ChildSAIn, s.ChildSAOut = s.espInboundSA, s.espOutboundSA
	s.ChildSAsIn = s.espInboundSAs
}

func configuredDuration(current time.Duration, legacySeconds int) time.Duration {
	if current > 0 {
		return current
	}
	if legacySeconds > 0 {
		return time.Duration(legacySeconds) * time.Second
	}
	return 0
}

func configuredTimerInterval(
	intervals []time.Duration,
	current time.Duration,
	legacySeconds int,
) time.Duration {
	if len(intervals) > 0 {
		return intervals[0]
	}
	return configuredDuration(current, legacySeconds)
}

func configuredProxyAddress(config *Config) string {
	if config == nil {
		return ""
	}
	if config.ProxyAddr != "" {
		return config.ProxyAddr
	}
	return config.Socks5Addr
}

func configuredProxyCredentials(config *Config) (username, password string) {
	if config == nil {
		return "", ""
	}
	if config.Proxy != nil {
		return config.Proxy.Username, config.Proxy.Password
	}
	return config.Socks5Username, config.Socks5Password
}

func (s *Session) reportProgress(message string) {
	if s != nil && s.cfg != nil && s.cfg.OnProgress != nil {
		s.cfg.OnProgress(message)
	}
}
