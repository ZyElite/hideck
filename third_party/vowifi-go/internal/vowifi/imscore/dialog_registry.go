package imscore

import (
	"errors"
	"sync"

	"github.com/emiago/sipgo"
	"github.com/emiago/sipgo/sip"
)

type dialogRegistry struct {
	mu      sync.RWMutex
	handles map[string]*imscoreDialogHandle
}

type inboundDialogReadResult struct {
	matched    bool
	responded  bool
	terminated bool
	handle     *imscoreDialogHandle
	err        error
}

func newDialogRegistry() *dialogRegistry {
	return &dialogRegistry{handles: make(map[string]*imscoreDialogHandle)}
}

func (r *dialogRegistry) store(handle *imscoreDialogHandle) {
	if r == nil || handle == nil || handle.id == "" {
		return
	}
	r.mu.Lock()
	r.handles[handle.id] = handle
	r.mu.Unlock()
}

func (r *dialogRegistry) load(id string) *imscoreDialogHandle {
	if r == nil || id == "" {
		return nil
	}
	r.mu.RLock()
	handle := r.handles[id]
	r.mu.RUnlock()
	return handle
}

func (r *dialogRegistry) delete(id string) {
	if r == nil || id == "" {
		return
	}
	r.mu.Lock()
	delete(r.handles, id)
	r.mu.Unlock()
}

func (r *dialogRegistry) len() int {
	if r == nil {
		return 0
	}
	r.mu.RLock()
	length := len(r.handles)
	r.mu.RUnlock()
	return length
}

func (r *dialogRegistry) matchInboundRequest(request *sip.Request) *imscoreDialogHandle {
	for _, id := range inboundDialogCandidateIDs(request) {
		if handle := r.load(id); handle != nil {
			return handle
		}
	}
	return nil
}

func inboundDialogCandidateIDs(request *sip.Request) []string {
	if request == nil {
		return nil
	}
	ids := make([]string, 0, 2)
	for _, makeID := range []func(*sip.Request) (string, error){
		sip.DialogIDFromRequestUAC,
		sip.DialogIDFromRequestUAS,
	} {
		id, err := makeID(request)
		if err == nil && id != "" && !containsDialogID(ids, id) {
			ids = append(ids, id)
		}
	}
	return ids
}

func containsDialogID(ids []string, candidate string) bool {
	for _, id := range ids {
		if id == candidate {
			return true
		}
	}
	return false
}

func (r *dialogRegistry) readInboundRequest(
	request *sip.Request,
	transaction sip.ServerTransaction,
) inboundDialogReadResult {
	handle := r.matchInboundRequest(request)
	if handle == nil {
		return inboundDialogReadResult{}
	}
	result := inboundDialogReadResult{matched: true, handle: handle}
	switch request.Method {
	case sip.ACK:
		result.err = readInboundDialogACK(handle, request)
	case sip.BYE:
		result.responded, result.terminated, result.err = r.readInboundDialogBYE(handle, request, transaction)
	case sip.INFO, sip.UPDATE:
		result.err = readInboundDialogSequence(handle, request)
	}
	return result
}

func readInboundDialogACK(handle *imscoreDialogHandle, request *sip.Request) error {
	if handle.server == nil {
		return nil
	}
	cseq := request.CSeq()
	if cseq == nil || handle.inviteRequest == nil || handle.inviteRequest.CSeq() == nil {
		return sipgo.ErrDialogInvalidCseq
	}
	if cseq.SeqNo != handle.inviteRequest.CSeq().SeqNo {
		return sipgo.ErrDialogInvalidCseq
	}
	handle.mu.Lock()
	if !handle.confirmed {
		handle.confirmed = true
		if handle.confirmedCh != nil {
			close(handle.confirmedCh)
		}
	}
	handle.mu.Unlock()
	return nil
}

func (r *dialogRegistry) readInboundDialogBYE(
	handle *imscoreDialogHandle,
	request *sip.Request,
	transaction sip.ServerTransaction,
) (bool, bool, error) {
	if err := readInboundDialogBYESequence(handle, request); err != nil {
		return false, false, err
	}
	if transaction == nil {
		return false, false, errors.New("dialog BYE server transaction is nil")
	}
	response := sip.NewResponseFromRequest(request, 200, "OK", nil)
	if err := transaction.Respond(response); err != nil {
		return false, false, err
	}
	r.delete(handle.id)
	return true, true, closeDialogSessions(handle)
}

func readInboundDialogBYESequence(handle *imscoreDialogHandle, request *sip.Request) error {
	if handle == nil || request == nil || request.CSeq() == nil {
		return sipgo.ErrDialogInvalidCseq
	}
	if handle.server == nil {
		return readInboundDialogSequence(handle, request)
	}
	if handle.inviteRequest == nil || handle.inviteRequest.CSeq() == nil {
		return sipgo.ErrDialogInvalidCseq
	}
	if request.CSeq().SeqNo < handle.inviteRequest.CSeq().SeqNo {
		return sipgo.ErrDialogInvalidCseq
	}
	return nil
}

func readInboundDialogSequence(handle *imscoreDialogHandle, request *sip.Request) error {
	if handle == nil || request == nil || request.CSeq() == nil {
		return sipgo.ErrDialogInvalidCseq
	}
	sequence := request.CSeq().SeqNo
	handle.mu.Lock()
	defer handle.mu.Unlock()
	if sequence < handle.remoteCSeq {
		return sipgo.ErrDialogInvalidCseq
	}
	handle.remoteCSeq = sequence
	return nil
}

func (r *dialogRegistry) closeAll() {
	if r == nil {
		return
	}
	r.mu.Lock()
	handles := make([]*imscoreDialogHandle, 0, len(r.handles))
	for _, handle := range r.handles {
		handles = append(handles, handle)
	}
	r.handles = make(map[string]*imscoreDialogHandle)
	r.mu.Unlock()
	for _, handle := range handles {
		_ = closeDialogSessions(handle)
	}
}

func closeDialogSessions(handle *imscoreDialogHandle) error {
	if handle == nil {
		return nil
	}
	handle.mu.Lock()
	if handle.closed {
		handle.mu.Unlock()
		return nil
	}
	handle.closed = true
	client, server := handle.client, handle.server
	handle.mu.Unlock()
	if client != nil {
		return client.Close()
	}
	if server != nil {
		return server.Close()
	}
	return nil
}
