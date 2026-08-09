package imscore

import (
	"strings"

	"github.com/emiago/sipgo/sip"
)

var dialogOwnedHeaders = [...]string{
	"Via", "Route", "From", "To", "Call-ID", "CSeq", "Max-Forwards", "Contact",
}

func stripDialogOwnedHeaders(request *sip.Request) *sip.Request {
	cloned := request.Clone()
	for _, name := range dialogOwnedHeaders {
		for cloned.RemoveHeader(name) {
		}
	}
	return cloned
}

func applyDialogRecipient(request *sip.Request, handle *imscoreDialogHandle) {
	if request.Recipient.Host == "" && handle.remoteTarget.Host != "" {
		request.Recipient = *handle.remoteTarget.Clone()
	}
}

func applyDialogCoreHeaders(s *Service, request *sip.Request, handle *imscoreDialogHandle) {
	request.PrependHeader(newDialogVia(s, handle))
	for _, route := range handle.routeSet {
		request.AppendHeader(sip.NewHeader("Route", route))
	}
	request.AppendHeader(dialogFromHeader(handle))
	request.AppendHeader(dialogToHeader(handle))
	callID := sip.CallIDHeader(handle.callID)
	request.AppendHeader(&callID)
	request.AppendHeader(&sip.CSeqHeader{
		SeqNo: nextDialogCSeqLocked(handle, request.Method), MethodName: request.Method,
	})
	maxForwards := sip.MaxForwardsHeader(dialogMaxForwards)
	request.AppendHeader(&maxForwards)
	if handle.localContact != nil {
		request.AppendHeader(handle.localContact.Clone())
	}
}

func nextDialogCSeqLocked(handle *imscoreDialogHandle, method sip.RequestMethod) uint32 {
	if method != sip.ACK && method != sip.CANCEL {
		handle.localCSeq++
	}
	return handle.localCSeq
}

func dialogFromHeader(handle *imscoreDialogHandle) sip.Header {
	if handle.client != nil {
		return sip.HeaderClone(handle.inviteRequest.From())
	}
	from := handle.inviteResponse.To().AsFrom()
	return &from
}

func dialogToHeader(handle *imscoreDialogHandle) sip.Header {
	if handle.client != nil {
		return sip.HeaderClone(handle.inviteResponse.To())
	}
	to := handle.inviteRequest.From().AsTo()
	return &to
}

func newDialogVia(s *Service, handle *imscoreDialogHandle) *sip.ViaHeader {
	var via *sip.ViaHeader
	if handle.client != nil && handle.inviteRequest.Via() != nil {
		via = handle.inviteRequest.Via().Clone()
		via.Params.Remove("received")
	} else {
		via = localDialogVia(s, handle)
	}
	via.Params.Remove("branch")
	via.Params.Add("branch", sip.RFC3261BranchMagicCookie+newBranch())
	return via
}

func localDialogVia(s *Service, handle *imscoreDialogHandle) *sip.ViaHeader {
	host, port := dialogContactAddress(handle)
	if s != nil && s.cfg != nil {
		if len(s.cfg.LocalIP) > 0 {
			host = s.cfg.LocalIP.String()
		}
		if s.cfg.LocalPort > 0 {
			port = s.cfg.LocalPort
		}
	}
	transport := strings.ToUpper(strings.TrimSpace(handle.inviteRequest.Transport()))
	if transport == "" {
		transport = "UDP"
	}
	params := sip.NewParams()
	params.Add("rport", "")
	return &sip.ViaHeader{
		ProtocolName: "SIP", ProtocolVersion: "2.0", Transport: transport,
		Host: host, Port: port, Params: params,
	}
}

func dialogContactAddress(handle *imscoreDialogHandle) (string, int) {
	if handle.localContact == nil {
		return "", 0
	}
	return handle.localContact.Address.Host, handle.localContact.Address.Port
}

func (s *Service) dialogSenderLocked(handle *imscoreDialogHandle) func(string) error {
	if handle.sender != nil {
		return handle.sender
	}
	if s == nil || s.transport == nil {
		return nil
	}
	s.transport.mu.Lock()
	sender := s.transport.sendFn
	s.transport.mu.Unlock()
	return sender
}
