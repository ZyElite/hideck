package imscore

import (
	"log/slog"
	"net"
)

// SIPOption configures the IMS SIP stack (recovered from the binary's
// sipgo-style With* option chain).
type SIPOption func(*sipOptions)

// sipOptions carries the SIP stack configuration.
type sipOptions struct {
	userAgent    string
	clientConn   string
	clientHost   string
	clientPort   int
	clientNAT    string
	terminateOnConnClose bool
	unhandledRespHandler interface{}
	transportLogger      *slog.Logger
}

// WithUserAgent sets the User-Agent header value.
func WithUserAgent(ua string) SIPOption {
	return func(o *sipOptions) { o.userAgent = ua }
}

// WithClientConnectionAddr sets the local client connection address.
func WithClientConnectionAddr(addr string) SIPOption {
	return func(o *sipOptions) { o.clientConn = addr }
}

// WithClientHostname sets the local client hostname.
func WithClientHostname(host string) SIPOption {
	return func(o *sipOptions) { o.clientHost = host }
}

// WithClientPort sets the local client port.
func WithClientPort(port int) SIPOption {
	return func(o *sipOptions) { o.clientPort = port }
}

// WithClientNAT sets the client NAT traversal mode.
func WithClientNAT(mode string) SIPOption {
	return func(o *sipOptions) { o.clientNAT = mode }
}

// WithTransactionLayerTerminateOnConnClose terminates transactions when the
// underlying connection closes.
func WithTransactionLayerTerminateOnConnClose(b bool) SIPOption {
	return func(o *sipOptions) { o.terminateOnConnClose = b }
}

// WithTransactionLayerUnhandledResponseHandler sets the handler for
// unhandled responses.
func WithTransactionLayerUnhandledResponseHandler(h interface{}) SIPOption {
	return func(o *sipOptions) { o.unhandledRespHandler = h }
}

// WithTransportLayerLogger sets the transport logger.
func WithTransportLayerLogger(l *slog.Logger) SIPOption {
	return func(o *sipOptions) { o.transportLogger = l }
}

// WithUserAgentTransactionLayerOptions sets transaction-layer options.
func WithUserAgentTransactionLayerOptions(opts ...SIPOption) SIPOption {
	return func(o *sipOptions) {
		for _, opt := range opts {
			if opt != nil {
				opt(o)
			}
		}
	}
}

// WithUserAgentTransportLayerOptions sets transport-layer options.
func WithUserAgentTransportLayerOptions(opts ...SIPOption) SIPOption {
	return func(o *sipOptions) {
		for _, opt := range opts {
			if opt != nil {
				opt(o)
			}
		}
	}
}

// defaultSIPOptions returns the default SIP stack options.
func defaultSIPOptions() *sipOptions {
	return &sipOptions{clientPort: 5060}
}

// applySIPOptions applies the options to the service config.
func (s *Service) applySIPOptions(opts ...SIPOption) {
	if s == nil || s.cfg == nil {
		return
	}
	o := defaultSIPOptions()
	for _, opt := range opts {
		if opt != nil {
			opt(o)
		}
	}
	if o.userAgent != "" {
		s.cfg.UserAgent = o.userAgent
	}
	if o.clientConn != "" {
		s.cfg.LocalIP = net.ParseIP(hostOnly(o.clientConn))
	}
}

// hostOnly extracts the host part of a host[:port] string.
func hostOnly(addr string) string {
	for i := 0; i < len(addr); i++ {
		if addr[i] == ':' {
			return addr[:i]
		}
	}
	return addr
}
