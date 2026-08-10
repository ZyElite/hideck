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
			response, seq, err := s.dispatchOutboundRequestWithCallbacks(
				outboundDispatchOptions{
					Context: task.ctx, Flow: task.flow, Request: task.req,
					Timeout: time.Duration(task.timeout), Callbacks: task.callbacks,
				},
				true,
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
	return s.dispatchOutboundMESSAGEWithCallbacks(
		outboundDispatchOptions{Context: ctx, Flow: flow, Request: req, Timeout: timeout},
	)
}

func (s *Service) dispatchOutboundMESSAGEWithCallbacks(
	options outboundDispatchOptions,
) (outboundMessageResult, error) {
	if options.Request == nil {
		return outboundMessageResult{}, errors.New("imscore: nil outbound MESSAGE")
	}
	if options.Context == nil {
		options.Context = context.Background()
	}
	s.ensureOutboundRequestDispatchers()
	task := outboundMessageTask{
		ctx: options.Context, flow: options.Flow,
		req: options.Request.Clone(), timeout: int64(options.Timeout),
		callbacks: options.Callbacks,
		done:      make(chan outboundMessageReply, 1),
	}
	select {
	case <-options.Context.Done():
		return outboundMessageResult{}, options.Context.Err()
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
	case <-options.Context.Done():
		return outboundMessageResult{}, options.Context.Err()
	case <-s.stop:
		return outboundMessageResult{}, errors.New("imscore: service stopped")
	}
}
