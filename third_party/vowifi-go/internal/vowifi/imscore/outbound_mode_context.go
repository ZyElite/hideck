package imscore

import (
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"

	"github.com/emiago/sipgo"
	"github.com/emiago/sipgo/sip"
)

// outboundModeContext is the immutable signaling-route snapshot captured for
// one outbound request in v1.5.5.
type outboundModeContext struct {
	Kind                    string
	Mode                    string
	IPSec3GPP               bool
	Config                  IMSConfig
	Generation              uint64
	SignalingReady          bool
	SignalingNotReadyReason string
	LocalIP                 string
	LocalHost               string
	LocalPortC              int
	LocalPortS              int
	RemoteIP                string
	RemotePortS             int
	Registrar               string
	ServiceRoute            string
	RouteHeader             string
	SecVerify               string
	ContactID               string
	PANI                    string
	AOR                     string
	Transport               string
	TCPConn                 net.Conn
	UDPConn                 net.PacketConn
	Client                  *sipgo.Client
	SkipGenericRawLog       bool
	send                    func(string) error
}

func (s *Service) resolveOutboundModeContext(
	flow string,
	req *sip.Request,
) (outboundModeContext, error) {
	if s == nil || s.transport == nil {
		return outboundModeContext{}, newOutboundModeResolveError("missing_sip_transport")
	}
	if req == nil {
		return outboundModeContext{}, errors.New("imscore: nil outbound request")
	}
	pani := s.GetPAccessNetworkInfo()
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.cfg == nil {
		return outboundModeContext{}, newOutboundModeResolveError("missing_ims_config")
	}
	modeCtx := s.outboundModeSnapshotLocked(flow, req, pani)
	sender := s.outboundSenderLocked(&modeCtx)
	if sender == nil {
		return outboundModeContext{}, newOutboundModeResolveError("missing_direct_sender")
	}
	if modeCtx.IPSec3GPP && !modeCtx.SignalingReady && modeCtx.Mode != "external" {
		reason := strings.TrimSpace(modeCtx.SignalingNotReadyReason)
		if reason == "" {
			reason = "signaling_not_ready"
		}
		return outboundModeContext{}, newOutboundModeResolveError(reason)
	}
	modeCtx.send = sender
	return modeCtx, nil
}

func (s *Service) outboundModeSnapshotLocked(
	flow string,
	req *sip.Request,
	pani string,
) outboundModeContext {
	cfg := cloneOutboundIMSConfig(s.cfg)
	route := s.registeredSIPRouteLocked()
	localIP := ""
	if cfg.LocalIP != nil {
		localIP = cfg.LocalIP.String()
	}
	modeCtx := outboundModeContext{
		Kind: strings.TrimSpace(flow), Config: cfg, Generation: s.signalingGeneration,
		SignalingReady: s.signalingReady, SignalingNotReadyReason: s.signalingFailureReason,
		IPSec3GPP: s.effectiveSecurityModeLocked() == securityModeIPSec,
		LocalIP:   localIP, LocalHost: strings.TrimSpace(cfg.LocalAddr),
		LocalPortC: s.protectedClientPort, LocalPortS: s.protectedServerPort,
		Registrar: strings.TrimSpace(s.registrar), ServiceRoute: route.serviceRoute,
		RouteHeader: route.serviceRoute, SecVerify: route.securityVerify,
		PANI: pani, Transport: strings.ToLower(strings.TrimSpace(req.Transport())),
		TCPConn: s.registrationTCP, UDPConn: s.registrationIO,
	}
	if modeCtx.LocalHost == "" {
		modeCtx.LocalHost = modeCtx.LocalIP
	}
	if s.regSession != nil {
		modeCtx.ContactID = strings.TrimSpace(s.regSession.contactUser)
		modeCtx.AOR = strings.TrimSpace(s.regSession.publicID)
	}
	if modeCtx.Registrar == "" {
		modeCtx.Registrar = strings.TrimSpace(cfg.Registrar)
	}
	if modeCtx.AOR == "" {
		modeCtx.AOR = firstNonBlank(cfg.publicIdentities()...)
	}
	s.populateOutboundRemoteLocked(&modeCtx)
	return modeCtx
}

func (s *Service) populateOutboundRemoteLocked(modeCtx *outboundModeContext) {
	if modeCtx.TCPConn != nil {
		modeCtx.Mode = "tcp"
		modeCtx.RemoteIP, modeCtx.RemotePortS = splitOutboundAddress(modeCtx.TCPConn.RemoteAddr())
		return
	}
	if modeCtx.UDPConn != nil && s.registrationRemote != nil {
		modeCtx.Mode = "udp"
		if s.registrationRemote.IP != nil {
			modeCtx.RemoteIP = s.registrationRemote.IP.String()
		}
		modeCtx.RemotePortS = s.registrationRemote.Port
		return
	}
	modeCtx.Mode = "external"
}

func (s *Service) outboundSenderLocked(modeCtx *outboundModeContext) func(string) error {
	if modeCtx.TCPConn != nil {
		conn := modeCtx.TCPConn
		return func(raw string) error { return s.writeSIPStream(conn, raw) }
	}
	if modeCtx.UDPConn != nil && s.registrationRemote != nil {
		packet := modeCtx.UDPConn
		remote := cloneUDPAddr(s.registrationRemote)
		return func(raw string) error {
			if _, err := packet.WriteTo([]byte(raw), remote); err != nil {
				return fmt.Errorf("imscore: write SIP datagram: %w", err)
			}
			return nil
		}
	}
	s.transport.mu.Lock()
	sender := s.transport.sendFn
	s.transport.mu.Unlock()
	return sender
}

func cloneOutboundIMSConfig(source *IMSConfig) IMSConfig {
	if source == nil {
		return IMSConfig{}
	}
	cloned := *source
	cloned.LocalIP = append(net.IP(nil), source.LocalIP...)
	cloned.IMPUs = append([]string(nil), source.IMPUs...)
	if source.EnableIPSec3GPP != nil {
		enabled := *source.EnableIPSec3GPP
		cloned.EnableIPSec3GPP = &enabled
	}
	return cloned
}

func splitOutboundAddress(address net.Addr) (string, int) {
	if address == nil {
		return "", 0
	}
	host, port, err := net.SplitHostPort(address.String())
	if err != nil {
		return "", 0
	}
	parsedPort, _ := strconv.Atoi(port)
	return strings.Trim(host, "[]"), parsedPort
}
