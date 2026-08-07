// Package ussi implements USSD over IMS SIP dialogs (3GPP TS 24.390).
package ussi

import (
	"context"
	"errors"
	"sync"
)

const (
	ContentType        = "application/vnd.3gpp.ussd+xml"
	InfoPackage        = "g.3gpp.ussd"
	ContentDisposition = "info-package"
)

// Transport is the authenticated IMS SIP transport used by USSI.
type Transport interface {
	RoundTrip(context.Context, string) (Response, error)
	Send(context.Context, string) error
}

// Response is the final response to a SIP transaction.
type Response struct {
	StatusCode int
	Reason     string
	Headers    map[string]string
	Body       []byte
}

// Config describes the registered IMS dialog endpoint.
type Config struct {
	Transport      Transport
	LocalURI       string
	ContactURI     string
	Domain         string
	LocalAddress   string
	SIPTransport   string
	ServiceRoute   string
	SecurityVerify string
	PANI           string
	UserAgent      string
	OnResult       func(Result)
}

// Result is a network-provided USSD result.
type Result struct {
	SessionID string
	Code      string
	Message   string
	RawXML    string
	Done      bool
}

type resultEvent struct {
	result Result
	err    error
}

// Session is one active USSI dialog.
type Session struct {
	mu           sync.RWMutex
	id           string
	callID       string
	localTag     string
	remoteTag    string
	remoteURI    string
	remoteTarget string
	routeSet     []string
	inviteBranch string
	cseq         int
	active       bool
	results      chan resultEvent
}

// ID returns the application session identifier.
func (s *Session) ID() string {
	if s == nil {
		return ""
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.id
}

// IsActive reports whether the network dialog is active.
func (s *Session) IsActive() bool {
	if s == nil {
		return false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.active
}

// Terminate marks the dialog terminal.
func (s *Session) Terminate() {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.active = false
	s.mu.Unlock()
}

// Service owns at most one active USSI dialog, matching the modem-facing API.
type Service struct {
	mu       sync.RWMutex
	cfg      Config
	sessions map[string]*Session
	active   *Session
	stopped  bool
}

// NewService preserves the original zero-argument constructor. Configure must
// be called before network operations.
func NewService() *Service {
	return &Service{sessions: make(map[string]*Session)}
}

// NewServiceWithConfig creates a configured USSI service.
func NewServiceWithConfig(cfg Config) (*Service, error) {
	s := NewService()
	if err := s.Configure(cfg); err != nil {
		return nil, err
	}
	return s, nil
}

// Configure installs the registered IMS endpoint.
func (s *Service) Configure(cfg Config) error {
	if s == nil {
		return errors.New("ussi: nil service")
	}
	if cfg.Transport == nil {
		return errors.New("ussi: SIP transport is required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.active != nil && s.active.IsActive() {
		return errors.New("ussi: cannot reconfigure an active session")
	}
	s.cfg = cfg
	s.stopped = false
	return nil
}

// InboundRequest is an in-dialog request received from the IMS network.
type InboundRequest struct {
	Method      string
	CallID      string
	ContentType string
	InfoPackage string
	Body        []byte
}

// InboundResult describes whether USSI consumed an inbound request.
type InboundResult struct {
	Handled    bool
	StatusCode int
	Result     *Result
}
