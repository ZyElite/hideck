package imscore

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/emiago/sipgo"
	"github.com/emiago/sipgo/sip"
	"github.com/iniwex5/vowifi-go/internal/vowifi/imsendpoint"
)

// DialogHandle is the exported dialog handle used by the voice layer.
type DialogHandle = imscoreDialogHandle

// InviteHandle is the exported invite handle used by the voice layer.
type InviteHandle = imscoreInviteHandle

// imscoreDialogHandle identifies a SIP dialog.
type imscoreDialogHandle struct {
	id     string
	client *sipgo.DialogClientSession
	server *sipgo.DialogServerSession

	mu             sync.Mutex
	callID         string
	fromTag        string
	toTag          string
	inviteRequest  *sip.Request
	inviteResponse *sip.Response
	routeSet       []string
	localContact   *sip.ContactHeader
	remoteTarget   sip.Uri
	sender         func(string) error
	localCSeq      uint32
	remoteCSeq     uint32
	confirmed      bool
	confirmedCh    chan struct{}
	inviteTx       sip.ServerTransaction
	closed         bool
}

// DialogID returns the dialog ID.
func (h *imscoreDialogHandle) DialogID() string {
	if h == nil {
		return ""
	}
	return h.id
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
	id             string
	dialog         *sipgo.DialogClientSession
	initialRequest *sip.Request
	mode           outboundModeContext
	mu             sync.Mutex
	done           bool
	confirmed      bool
	canceling      bool
	cancelSent     bool

	transaction *clientSIPTransaction
}

// InviteID returns the invite ID.
func (h *imscoreInviteHandle) InviteID() string {
	if h == nil {
		return ""
	}
	return h.id
}

func (h *imscoreInviteHandle) markDone(confirmed bool) {
	if h == nil {
		return
	}
	h.mu.Lock()
	h.done = true
	h.confirmed = confirmed
	h.canceling = false
	h.mu.Unlock()
}

// imscoreServerInviteHandle identifies a server INVITE transaction.
type imscoreServerInviteHandle struct {
	id        string
	req       *sip.Request
	tx        sip.ServerTransaction
	mu        sync.Mutex
	responded bool
	runtime   *serverSIPTransaction
}

// InviteID returns the invite ID.
func (h *imscoreServerInviteHandle) InviteID() string {
	if h == nil {
		return ""
	}
	return h.id
}

// imscoreInboundRequestHandle identifies an inbound request.
type imscoreInboundRequestHandle struct {
	id        string
	req       *sip.Request
	tx        sip.ServerTransaction
	mu        sync.Mutex
	responded bool
	runtime   *serverSIPTransaction
}

// Method returns the request method.
func (h *imscoreInboundRequestHandle) Method() string {
	if h == nil {
		return ""
	}
	if h.req == nil {
		return ""
	}
	return string(h.req.Method)
}

// inboundRequestResponseMemo caches a response to an inbound request.
type inboundRequestResponseMemo struct {
	Code   int
	Reason string
	At     time.Time
}

// CancelClientInviteRaw retains the additive handle-only cancellation API.
func (s *Service) CancelClientInviteRaw(handle *imscoreInviteHandle) error {
	if s == nil || handle == nil {
		return errors.New("imscore: client INVITE handle is required")
	}
	if s.transport == nil {
		return errors.New("imscore: client INVITE SIP client is empty")
	}
	handle.mu.Lock()
	transaction := handle.transaction
	switch {
	case handle.confirmed:
		handle.mu.Unlock()
		return errors.New("client INVITE 已接通，不能 CANCEL")
	case handle.done:
		handle.mu.Unlock()
		return errors.New("client INVITE 已结束，不能 CANCEL")
	case handle.canceling || handle.cancelSent:
		handle.mu.Unlock()
		return errors.New("client INVITE 已在取消中")
	case transaction == nil:
		handle.mu.Unlock()
		return errors.New("client INVITE initial request 为空")
	}
	handle.canceling = true
	handle.mu.Unlock()
	err := s.transport.cancelInviteTransaction(transaction, s.transport.transactionTimers().bf)
	handle.mu.Lock()
	handle.canceling = false
	handle.cancelSent = err == nil
	handle.mu.Unlock()
	return err
}

// CancelClientInvite cancels a v1.5.5 client INVITE transaction.
func (s *Service) CancelClientInvite(
	ctx context.Context,
	_ string,
	invite imsendpoint.InviteHandle,
	options imsendpoint.ClientInviteCancelOptions,
) error {
	handle, ok := invite.(*imscoreInviteHandle)
	if !ok || handle == nil {
		return errors.New("client INVITE handle 类型无效")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	return s.cancelClientInviteWithContext(ctx, handle, options)
}

// CancelClientInviteForCurrentDevice retains the additive device-implicit API.
func (s *Service) CancelClientInviteForCurrentDevice(
	ctx context.Context,
	invite imsendpoint.InviteHandle,
	options imsendpoint.ClientInviteCancelOptions,
) error {
	return s.CancelClientInvite(ctx, s.DeviceID(), invite, options)
}

// CloseDialogRaw retains the additive handle-only close API.
func (s *Service) CloseDialogRaw(handle *imscoreDialogHandle) error {
	if s == nil || handle == nil {
		return errors.New("imscore: dialog handle is required")
	}
	return s.closeDialogHandle(handle)
}

// NextDialogCSeqRaw retains the additive per-dialog CSeq helper.
func (s *Service) NextDialogCSeqRaw(handle *imscoreDialogHandle) int {
	if s == nil || handle == nil {
		return 1
	}
	handle.mu.Lock()
	defer handle.mu.Unlock()
	handle.localCSeq++
	return int(handle.localCSeq)
}
