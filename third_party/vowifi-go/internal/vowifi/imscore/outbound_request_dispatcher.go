package imscore

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/emiago/sipgo/sip"
	"github.com/iniwex5/vowifi-go/internal/vowifi/logging"
)

const (
	outboundRequestShardCount    = 16
	outboundRequestQueueCapacity = 128
	outboundQueueWarningDelay    = time.Second
)

var errOutboundRequestQueueFull = errors.New("smsip_outbound_queue_full: outbound request queue full")

func (s *Service) initOutboundRequestDispatchersLocked() {
	if s == nil || len(s.outboundReqShards) != 0 {
		return
	}
	s.outboundReqShards = make([]chan outboundRequestTask, outboundRequestShardCount)
	for shard := range s.outboundReqShards {
		queue := make(chan outboundRequestTask, outboundRequestQueueCapacity)
		s.outboundReqShards[shard] = queue
		s.networkDone.Add(1)
		go s.runOutboundRequestShard(shard, queue)
	}
	if s.outboundMsgCh == nil {
		s.outboundMsgCh = make(chan outboundMessageTask, outboundRequestQueueCapacity)
		s.networkDone.Add(1)
		go s.runOutboundMessageDispatcher()
	}
}

func (s *Service) ensureOutboundRequestDispatchers() []chan outboundRequestTask {
	if s == nil {
		return nil
	}
	s.outboundMu.Lock()
	s.initOutboundRequestDispatchersLocked()
	shards := append([]chan outboundRequestTask(nil), s.outboundReqShards...)
	s.outboundMu.Unlock()
	return shards
}

func (s *Service) runOutboundRequestShard(shard int, queue <-chan outboundRequestTask) {
	defer s.networkDone.Done()
	for {
		select {
		case <-s.stop:
			return
		case task, ok := <-queue:
			if !ok {
				return
			}
			s.runOutboundRequestTask(shard, task)
		}
	}
}

func (s *Service) runOutboundRequestTask(shard int, task outboundRequestTask) {
	if delay := time.Since(task.enqueuedAt); delay > outboundQueueWarningDelay {
		logging.WarnRate("smsip_outbound_queue_wait_high", "IMS outbound request queue delay is high",
			"shard", shard, "delay", delay, "dispatch_seq", task.dispatchSeq)
	}
	response, err := s.executeOutboundRequest(task.ctx, task.req, task.modeCtx, time.Duration(task.timeout))
	reply := outboundRequestReply{res: response, err: err, dispatchSeq: task.dispatchSeq}
	if task.done == nil {
		return
	}
	select {
	case task.done <- reply:
	default:
	}
}

func (s *Service) executeOutboundRequest(
	ctx context.Context,
	req *sip.Request,
	modeCtx outboundModeContext,
	timeout time.Duration,
) (*sip.Response, error) {
	if req == nil {
		return nil, errors.New("imscore: nil outbound request")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}
	response, err := s.transport.roundTripWithSender(ctx, req.String(), modeCtx.send)
	if err != nil {
		s.handleOutboundRequestError(req, err)
		return nil, err
	}
	if response == nil {
		return nil, errors.New("imscore: outbound request returned no SIP response")
	}
	if response.parsed == nil {
		return sip.NewResponse(response.StatusCode, response.Reason), nil
	}
	return response.parsed.Clone(), nil
}

func (s *Service) handleOutboundRequestError(req *sip.Request, err error) {
	if err == nil || !isFatalSIPTransportError(err) {
		return
	}
	method := "request"
	if req != nil && strings.TrimSpace(string(req.Method)) != "" {
		method = strings.TrimSpace(string(req.Method))
	}
	s.markSignalingDead(fmt.Errorf("outbound %s: %w", method, err))
}

func outboundDispatchKey(req *sip.Request, flow string) string {
	if req != nil && req.CallID() != nil {
		if callID := strings.TrimSpace(req.CallID().Value()); callID != "" {
			return "callid:" + callID
		}
	}
	method := "request"
	if req != nil && strings.TrimSpace(string(req.Method)) != "" {
		method = strings.ToUpper(strings.TrimSpace(string(req.Method)))
	}
	return "fallback:" + strings.TrimSpace(flow) + ":" + method
}

func outboundDispatchShardIndex(key string, shardCount int) int {
	if shardCount < 2 {
		return 0
	}
	const (
		fnvOffset32 = uint32(2166136261)
		fnvPrime32  = uint32(16777619)
	)
	hash := fnvOffset32
	for index := 0; index < len(key); index++ {
		hash = (hash ^ uint32(key[index])) * fnvPrime32
	}
	return int(hash % uint32(shardCount))
}

func (s *Service) dispatchOutboundRequest(
	ctx context.Context,
	flow string,
	req *sip.Request,
	timeout time.Duration,
	wait bool,
) (*sip.Response, uint64, error) {
	if req == nil {
		return nil, 0, errors.New("imscore: nil outbound request")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	modeCtx, err := s.resolveOutboundModeContext(flow, req)
	if err != nil {
		return nil, 0, err
	}
	shards := s.ensureOutboundRequestDispatchers()
	if len(shards) == 0 {
		return nil, 0, errors.New("imscore: outbound request dispatcher unavailable")
	}
	seq := s.outboundDispatchSeq.Add(1)
	done := make(chan outboundRequestReply, 1)
	if !wait {
		done = nil
	}
	task := outboundRequestTask{
		ctx: ctx, flow: flow, req: req.Clone(), timeout: int64(timeout),
		modeCtx: modeCtx, dispatchSeq: seq, enqueuedAt: time.Now(), done: done,
	}
	queue := shards[outboundDispatchShardIndex(outboundDispatchKey(req, flow), len(shards))]
	select {
	case <-ctx.Done():
		return nil, seq, ctx.Err()
	case <-s.stop:
		return nil, seq, errors.New("imscore: service stopped")
	case queue <- task:
	default:
		s.outboundQueueReject.Add(1)
		return nil, seq, errOutboundRequestQueueFull
	}
	if !wait {
		return nil, seq, nil
	}
	select {
	case reply := <-done:
		return reply.res, reply.dispatchSeq, reply.err
	case <-ctx.Done():
		return nil, seq, ctx.Err()
	case <-s.stop:
		return nil, seq, errors.New("imscore: service stopped")
	}
}

func (s *Service) buildOutboundRequest(req *sip.Request) (*sip.Request, error) {
	if req == nil {
		return nil, errors.New("imscore: nil outbound request")
	}
	if req.CallID() == nil || req.CSeq() == nil || req.Via() == nil {
		return nil, errors.New("imscore: outbound request is missing transaction headers")
	}
	return req.Clone(), nil
}
