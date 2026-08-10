package imscore

import (
	"context"
	"time"

	"github.com/iniwex5/vowifi-go/internal/vowifi/logging"
)

const defaultInboundStatsInterval = 30 * time.Second

type inboundStatsLoggerOptions struct {
	Interval time.Duration
}

type inboundStatsSnapshot struct {
	UDPSocketReads     uint64
	UDPSocketBytes     uint64
	TCPAccepts         uint64
	TCPSocketReads     uint64
	TCPSocketBytes     uint64
	SIPParsedMessages  uint64
	SIPParsedRequests  uint64
	SIPParsedResponses uint64
}

func (s *Service) startInboundStatsLogger(ctx context.Context, options inboundStatsLoggerOptions) {
	if s == nil || ctx == nil || options.Interval <= 0 {
		return
	}
	s.inboundStatsMu.Lock()
	defer s.inboundStatsMu.Unlock()
	s.stopInboundStatsLoggerLocked()
	if s.stopped() {
		return
	}
	loggerCtx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	s.inboundStatsCancel = cancel
	s.inboundStatsDone = done
	go func() {
		defer close(done)
		s.logInboundStats(loggerCtx, options.Interval)
	}()
}

func (s *Service) stopInboundStatsLogger() {
	if s == nil {
		return
	}
	s.inboundStatsMu.Lock()
	defer s.inboundStatsMu.Unlock()
	s.stopInboundStatsLoggerLocked()
}

func (s *Service) stopInboundStatsLoggerLocked() {
	cancel := s.inboundStatsCancel
	done := s.inboundStatsDone
	s.inboundStatsCancel = nil
	s.inboundStatsDone = nil
	if cancel != nil {
		cancel()
	}
	if done != nil {
		<-done
	}
}

func (s *Service) logInboundStats(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-s.stop:
			return
		case <-ticker.C:
			s.logInboundStatsSnapshot(s.captureInboundStats())
		}
	}
}

func (s *Service) logInboundStatsSnapshot(stats inboundStatsSnapshot) {
	status := s.receiverStatus()
	logging.Debug("IMS inbound stats",
		"device", s.DeviceID(), "transport", status.Transport,
		"local_address", status.LocalAddress, "local_port", s.cfg.LocalPort,
		"udp_socket_reads", stats.UDPSocketReads, "udp_socket_bytes", stats.UDPSocketBytes,
		"tcp_accepts", stats.TCPAccepts, "tcp_socket_reads", stats.TCPSocketReads,
		"tcp_socket_bytes", stats.TCPSocketBytes,
		"sip_parsed_messages", stats.SIPParsedMessages,
		"sip_parsed_requests", stats.SIPParsedRequests,
		"sip_parsed_responses", stats.SIPParsedResponses)
}

func (s *Service) captureInboundStats() inboundStatsSnapshot {
	if s == nil {
		return inboundStatsSnapshot{}
	}
	return inboundStatsSnapshot{
		UDPSocketReads:     s.inboundUDPSocketRead.Load(),
		UDPSocketBytes:     s.inboundUDPSocketBytes.Load(),
		TCPAccepts:         s.inboundTCPAccept.Load(),
		TCPSocketReads:     s.inboundTCPSocketRead.Load(),
		TCPSocketBytes:     s.inboundTCPSocketBytes.Load(),
		SIPParsedMessages:  s.inboundSIPParsedMessage.Load(),
		SIPParsedRequests:  s.inboundSIPParsedRequest.Load(),
		SIPParsedResponses: s.inboundSIPParsedResp.Load(),
	}
}

func (stats inboundStatsSnapshot) diagnostics() map[string]interface{} {
	return map[string]interface{}{
		"inbound_udp_socket_reads":     stats.UDPSocketReads,
		"inbound_udp_socket_bytes":     stats.UDPSocketBytes,
		"inbound_tcp_accepts":          stats.TCPAccepts,
		"inbound_tcp_socket_reads":     stats.TCPSocketReads,
		"inbound_tcp_socket_bytes":     stats.TCPSocketBytes,
		"inbound_sip_parsed_messages":  stats.SIPParsedMessages,
		"inbound_sip_parsed_requests":  stats.SIPParsedRequests,
		"inbound_sip_parsed_responses": stats.SIPParsedResponses,
	}
}
