package imscore

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/emiago/sipgo/sip"
	"github.com/iniwex5/vowifi-go/internal/vowifi/logging"
)

func (s *Service) runOutboundMessageDispatcher() {
	defer s.networkDone.Done()
	for {
		select {
		case <-s.stop:
			return
		case task, ok := <-s.outboundMsgCh:
			if !ok {
				return
			}
			logging.RunDebug("IMS outbound MESSAGE",
				"flow", task.flow, "call_id", outboundRequestCallID(task.req),
				"sip", outboundRequestDebugText(task.req))
			response, seq, err := s.dispatchOutboundRequest(
				task.ctx, task.flow, task.req, time.Duration(task.timeout), true,
			)
			result := outboundMessageResult{DispatchSeq: seq}
			if response != nil {
				result.SIPCode = response.StatusCode
			}
			select {
			case task.done <- outboundMessageReply{result: result, err: err}:
			default:
			}
		}
	}
}

func outboundRequestDebugText(request *sip.Request) string {
	if request == nil {
		return ""
	}
	return sipDebugRawText(request.String())
}

func outboundRequestCallID(request *sip.Request) string {
	if request == nil || request.CallID() == nil {
		return ""
	}
	return strings.TrimSpace(request.CallID().Value())
}

func (s *Service) dispatchOutboundMESSAGE(
	ctx context.Context,
	flow string,
	req *sip.Request,
	timeout time.Duration,
) (outboundMessageResult, error) {
	if req == nil {
		return outboundMessageResult{}, errors.New("imscore: nil outbound MESSAGE")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	s.ensureOutboundRequestDispatchers()
	task := outboundMessageTask{
		ctx: ctx, flow: flow, req: req.Clone(), timeout: int64(timeout),
		done: make(chan outboundMessageReply, 1),
	}
	select {
	case <-ctx.Done():
		return outboundMessageResult{}, ctx.Err()
	case <-s.stop:
		return outboundMessageResult{}, errors.New("imscore: service stopped")
	case s.outboundMsgCh <- task:
	default:
		s.outboundQueueReject.Add(1)
		return outboundMessageResult{}, errOutboundRequestQueueFull
	}
	select {
	case reply := <-task.done:
		return reply.result, reply.err
	case <-ctx.Done():
		return outboundMessageResult{}, ctx.Err()
	case <-s.stop:
		return outboundMessageResult{}, errors.New("imscore: service stopped")
	}
}
