package imscore

import (
	"errors"
	"sync"
)

// DialogHandle is the exported dialog handle used by the voice layer.
type DialogHandle = imscoreDialogHandle

// InviteHandle is the exported invite handle used by the voice layer.
type InviteHandle = imscoreInviteHandle

// imscoreDialogHandle identifies a SIP dialog.
type imscoreDialogHandle struct {
	callID  string
	fromTag string
	toTag   string
}

// DialogID returns the dialog ID.
func (h *imscoreDialogHandle) DialogID() string {
	if h == nil {
		return ""
	}
	return h.callID
}

// ToTag returns the remote tag.
func (h *imscoreDialogHandle) ToTag() string {
	if h == nil {
		return ""
	}
	return h.toTag
}

// FromTag returns the local tag.
func (h *imscoreDialogHandle) FromTag() string {
	if h == nil {
		return ""
	}
	return h.fromTag
}

// imscoreInviteHandle identifies an INVITE transaction.
type imscoreInviteHandle struct {
	callID string
}

// InviteID returns the invite ID.
func (h *imscoreInviteHandle) InviteID() string {
	if h == nil {
		return ""
	}
	return h.callID
}

// imscoreServerInviteHandle identifies a server INVITE transaction.
type imscoreServerInviteHandle struct {
	callID string
}

// InviteID returns the invite ID.
func (h *imscoreServerInviteHandle) InviteID() string {
	if h == nil {
		return ""
	}
	return h.callID
}

// imscoreInboundRequestHandle identifies an inbound request.
type imscoreInboundRequestHandle struct {
	method string
	callID string
}

// Method returns the request method.
func (h *imscoreInboundRequestHandle) Method() string {
	if h == nil {
		return ""
	}
	return h.method
}

// inboundRequestResponseMemo caches a response to an inbound request.
type inboundRequestResponseMemo struct {
	statusCode int
	headers    map[string]string
}

// dialogEntry is one registered dialog.
type dialogEntry struct {
	handle    *imscoreDialogHandle
	localTag  string
	remoteTag string
	cseq      int
	route     []string
}

// dialogRegistry stores in-progress dialogs.
type dialogRegistry struct {
	mu      sync.RWMutex
	dialogs map[string]*dialogEntry // keyed by call ID
}

// newDialogRegistry creates a dialog registry.
func newDialogRegistry() *dialogRegistry {
	return &dialogRegistry{dialogs: make(map[string]*dialogEntry)}
}

// store registers a dialog.
func (r *dialogRegistry) store(callID string, d *dialogEntry) {
	if r == nil {
		return
	}
	r.mu.Lock()
	r.dialogs[callID] = d
	r.mu.Unlock()
}

// load returns a dialog by call ID.
func (r *dialogRegistry) load(callID string) *dialogEntry {
	if r == nil {
		return nil
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.dialogs[callID]
}

// delete removes a dialog.
func (r *dialogRegistry) delete(callID string) {
	if r == nil {
		return
	}
	r.mu.Lock()
	delete(r.dialogs, callID)
	r.mu.Unlock()
}

// len returns the number of dialogs.
func (r *dialogRegistry) len() int {
	if r == nil {
		return 0
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.dialogs)
}

// matchInboundRequest finds a dialog matching an inbound request.
func (r *dialogRegistry) matchInboundRequest(callID, fromTag, toTag string) *dialogEntry {
	if r == nil {
		return nil
	}
	return r.load(callID)
}

// readInboundRequest reads the dialog for an inbound request.
func (r *dialogRegistry) readInboundRequest(callID string) *dialogEntry {
	return r.load(callID)
}

// inboundDialogCandidateIDs returns candidate dialog IDs for an inbound request.
func inboundDialogCandidateIDs(callID string) []string {
	return []string{callID}
}

// Service dialog methods.

// AnswerServerInvite answers a server-side INVITE.
func (s *Service) AnswerServerInvite(handle *imscoreServerInviteHandle) error {
	if s == nil || handle == nil {
		return errors.New("imscore: server INVITE handle is required")
	}
	return errors.New("imscore: server INVITE request context is unavailable")
}

// CancelClientInvite cancels a client-side INVITE.
func (s *Service) CancelClientInvite(handle *imscoreInviteHandle) error {
	if s == nil || handle == nil {
		return errors.New("imscore: client INVITE handle is required")
	}
	return errors.New("imscore: client INVITE transaction context is unavailable")
}

// CloseDialog closes a dialog.
func (s *Service) CloseDialog(handle *imscoreDialogHandle) error {
	if s == nil || s.dialogs == nil || handle == nil {
		return errors.New("imscore: dialog handle is required")
	}
	s.dialogs.delete(handle.DialogID())
	return nil
}

// NextCSeq returns the next CSeq for a dialog.
func (s *Service) NextCSeq(handle *imscoreDialogHandle) int {
	if s == nil || handle == nil || s.dialogs == nil {
		return 1
	}
	s.dialogs.mu.Lock()
	defer s.dialogs.mu.Unlock()
	d := s.dialogs.dialogs[handle.DialogID()]
	if d == nil {
		return 1
	}
	d.cseq++
	return d.cseq
}
