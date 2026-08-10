// Package ussi implements USSD over IMS SIP dialogs (3GPP TS 24.390).
package ussi

import (
	"sync"
	"time"

	"github.com/iniwex5/vowifi-go/internal/vowifi/imsendpoint"
)

const (
	ContentType        = "application/vnd.3gpp.ussd+xml"
	InfoPackage        = "g.3gpp.ussd"
	ContentDisposition = "info-package"

	sessionActive     = 1
	sessionTerminated = 2
)

// Context is the immutable registered IMS state used to construct USSI SIP.
type Context struct {
	LocalIP      string
	LocalPortC   int
	LocalPortS   int
	Transport    string
	Domain       string
	Realm        string
	AOR          string
	RouteHeader  string
	ServiceRoute string
	SecVerify    string
	Mode         string
	PANI         string
	UserAgent    string
	ContactID    string
	Destination  string
}

// InfoResult is one INFO or BYE result delivered to a waiting operation.
type InfoResult struct {
	Text   string
	RawXML string
	Err    error
}

// Result is the v1.5.5 USSD result contract.
type Result struct {
	Text      string
	Status    int
	SessionID string
	RawXML    string
	DCS       int
}

// Session is the single active USSI dialog owned by a Service.
type Session struct {
	mu            sync.Mutex
	ID            string
	CallID        string
	RemoteURI     string
	RemoteTarget  string
	State         int
	ResultCh      chan InfoResult
	CreatedAt     time.Time
	LastAt        time.Time
	dialogContext Context
	dialogHandle  imsendpoint.DialogHandle
}

// IsActive reports whether the dialog can accept another operation.
func (s *Session) IsActive() bool {
	if s == nil {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.State == sessionActive
}

// Terminate marks the dialog terminal and drops its endpoint handle.
func (s *Session) Terminate() {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.State = sessionTerminated
	s.dialogHandle = nil
	s.mu.Unlock()
}

// Service owns at most one active USSI dialog.
type Service struct {
	deviceID string
	endpoint imsendpoint.ClientDialogEndpoint
	mu       sync.Mutex
	session  *Session
}

// NewService binds USSI to the runtime-owned IMS endpoint.
func NewService(deviceID string, endpoint imsendpoint.ClientDialogEndpoint) *Service {
	return &Service{deviceID: deviceID, endpoint: endpoint}
}
