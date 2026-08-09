package imscore

import (
	"strconv"
	"strings"
	"time"

	"github.com/emiago/sipgo/sip"
)

const (
	inboundRequestMemoTTL = 2 * time.Minute
	inboundRequestMemoCap = 4096
)

func txKeyFromMsg(message sip.Message) string {
	if message == nil || message.CallID() == nil || message.CSeq() == nil || message.Via() == nil {
		return ""
	}
	branch, _ := message.Via().Params.Get("branch")
	branch = strings.TrimSpace(branch)
	if branch == "" {
		return ""
	}
	return strings.TrimSpace(message.CallID().Value()) + "|" +
		strings.TrimSpace(message.CSeq().Value()) + "|" + branch
}

func inboundRequestKey(request *sip.Request) string {
	if request == nil {
		return ""
	}
	if key := txKeyFromMsg(request); key != "" {
		return key
	}
	if request.CallID() == nil || request.CSeq() == nil {
		return ""
	}
	callID := strings.TrimSpace(request.CallID().Value())
	cseq := strings.TrimSpace(request.CSeq().Value())
	if callID == "" || cseq == "" {
		return ""
	}
	return callID + "|" + cseq
}

func serverTransactionKey(request *sip.Request, inviteForACK bool) string {
	if request == nil || request.CallID() == nil || request.CSeq() == nil || request.Via() == nil {
		return ""
	}
	branch, _ := request.Via().Params.Get("branch")
	method := strings.ToUpper(string(request.CSeq().MethodName))
	if inviteForACK && (method == "ACK" || method == "CANCEL") {
		method = "INVITE"
	}
	if strings.TrimSpace(branch) == "" || method == "" {
		return ""
	}
	return strings.TrimSpace(request.CallID().Value()) + "|" +
		strconv.FormatUint(uint64(request.CSeq().SeqNo), 10) + "|" + method + "|" + branch
}

func (s *Service) reserveInboundRequestWithMemo(
	request *sip.Request,
) (bool, *inboundRequestResponseMemo) {
	key := inboundRequestKey(request)
	if key == "" {
		return true, nil
	}
	now := time.Now()
	s.inboundSeenMu.Lock()
	defer s.inboundSeenMu.Unlock()
	s.ensureInboundMemoMapsLocked()
	if at, exists := s.inboundSeen[key]; exists && now.Sub(at) <= inboundRequestMemoTTL {
		memo, memoExists := s.inboundSeenRsp[key]
		if memoExists && now.Sub(memo.At) <= inboundRequestMemoTTL {
			copy := memo
			return false, &copy
		}
		return false, &inboundRequestResponseMemo{Code: 200, Reason: "OK", At: at}
	}
	s.inboundSeen[key] = now
	s.pruneInboundMemosLocked(now)
	return true, nil
}

func (s *Service) memoInboundRequestResponse(
	request *sip.Request,
	code int,
	reason string,
) {
	key := inboundRequestKey(request)
	if key == "" {
		return
	}
	s.inboundSeenMu.Lock()
	s.ensureInboundMemoMapsLocked()
	s.inboundSeenRsp[key] = inboundRequestResponseMemo{Code: code, Reason: strings.TrimSpace(reason), At: time.Now()}
	s.inboundSeenMu.Unlock()
}

func (s *Service) releaseInboundRequestReservation(request *sip.Request) {
	key := inboundRequestKey(request)
	if key == "" {
		return
	}
	s.inboundSeenMu.Lock()
	delete(s.inboundSeen, key)
	delete(s.inboundSeenRsp, key)
	s.inboundSeenMu.Unlock()
}

func (s *Service) ensureInboundMemoMapsLocked() {
	if s.inboundSeen == nil {
		s.inboundSeen = make(map[string]time.Time, 128)
	}
	if s.inboundSeenRsp == nil {
		s.inboundSeenRsp = make(map[string]inboundRequestResponseMemo, 128)
	}
}

func (s *Service) pruneInboundMemosLocked(now time.Time) {
	if len(s.inboundSeen) <= inboundRequestMemoCap {
		return
	}
	cutoff := now.Add(-inboundRequestMemoTTL)
	for key, at := range s.inboundSeen {
		if at.Before(cutoff) {
			delete(s.inboundSeen, key)
			delete(s.inboundSeenRsp, key)
		}
	}
}
