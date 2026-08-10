package imscore

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/emiago/sipgo/sip"
	"github.com/iniwex5/vowifi-go/internal/vowifi/policy"
)

const outboundDirectWriteTimeout = 5 * time.Second

var errIMSSignalingRuntimeNotReady = errors.New("IMS signaling runtime not ready")

type directTCPWriteOptions struct {
	Context context.Context
	Conn    net.Conn
	Payload []byte
	Timeout time.Duration
}

type outboundSendOperation struct {
	Context context.Context
	Mode    outboundModeContext
	Request *sip.Request
	Timeout time.Duration
}

func transportForRequest(configuredTransport, securityMode string, method sip.RequestMethod) string {
	transport := strings.ToUpper(policy.NormalizeIMSTransport(configuredTransport))
	protected := normalizeSecurityMode(securityMode) == securityModeIPSec
	if transport == "AUTO" {
		if protected {
			return "TCP"
		}
		return "UDP"
	}
	if protected && (method == sip.OPTIONS || method == sip.SUBSCRIBE) {
		return "TCP"
	}
	if transport == "" {
		return "TCP"
	}
	return transport
}

func signalingRuntimeNotReadyError(reason string) error {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		reason = "unknown"
	}
	return fmt.Errorf("%w: %s", errIMSSignalingRuntimeNotReady, reason)
}

func destinationFromContext(modeCtx outboundModeContext) string {
	if modeCtx.TCPConn != nil && modeCtx.TCPConn.RemoteAddr() != nil {
		return modeCtx.TCPConn.RemoteAddr().String()
	}
	host := strings.Trim(strings.TrimSpace(modeCtx.RemoteIP), "[]")
	if host == "" || modeCtx.RemotePortS < 1 {
		return ""
	}
	return net.JoinHostPort(host, strconv.Itoa(modeCtx.RemotePortS))
}

func modeContextErrorFields(err error) (code, message string) {
	if err == nil {
		return "", ""
	}
	code = outboundResolveErrorCode(err)
	if code == "" {
		code = "unknown"
	}
	return code, strings.TrimSpace(err.Error())
}

func sendDirectTCPSafe(options directTCPWriteOptions) error {
	if options.Conn == nil {
		return errors.New("TCPConn is nil for direct write")
	}
	deadline, err := outboundWriteDeadline(options.Context, options.Timeout)
	if err != nil {
		return err
	}
	if err := options.Conn.SetWriteDeadline(deadline); err != nil {
		return err
	}
	defer func() { _ = options.Conn.SetWriteDeadline(time.Time{}) }()
	written, err := options.Conn.Write(options.Payload)
	if err != nil {
		return outboundWriteError(options.Context, err)
	}
	if written != len(options.Payload) {
		return io.ErrShortWrite
	}
	return nil
}

func sendDirectWrite(ctx context.Context, modeCtx outboundModeContext, raw string) error {
	payload := []byte(raw)
	if modeCtx.TCPConn != nil {
		return sendDirectTCPSafe(directTCPWriteOptions{
			Context: ctx, Conn: modeCtx.TCPConn, Payload: payload, Timeout: outboundDirectWriteTimeout,
		})
	}
	if modeCtx.UDPConn == nil {
		return errors.New("no direct write channel available")
	}
	destination := destinationFromContext(modeCtx)
	remote, err := net.ResolveUDPAddr("udp", destination)
	if err != nil {
		return err
	}
	deadline, err := outboundWriteDeadline(ctx, outboundDirectWriteTimeout)
	if err != nil {
		return err
	}
	if err := modeCtx.UDPConn.SetWriteDeadline(deadline); err != nil {
		return err
	}
	defer func() { _ = modeCtx.UDPConn.SetWriteDeadline(time.Time{}) }()
	_, err = modeCtx.UDPConn.WriteTo(payload, remote)
	return outboundWriteError(ctx, err)
}

func outboundWriteDeadline(ctx context.Context, timeout time.Duration) (time.Time, error) {
	if timeout <= 0 {
		timeout = outboundDirectWriteTimeout
	}
	deadline := time.Now().Add(timeout)
	if ctx == nil {
		return deadline, nil
	}
	if err := ctx.Err(); err != nil {
		return time.Time{}, fmt.Errorf("%w: %v", err, err)
	}
	if contextDeadline, ok := ctx.Deadline(); ok && contextDeadline.Before(deadline) {
		deadline = contextDeadline
	}
	return deadline, nil
}

func outboundWriteError(ctx context.Context, err error) error {
	if err == nil {
		return nil
	}
	if ctx != nil && ctx.Err() != nil {
		return fmt.Errorf("%w: %v", ctx.Err(), err)
	}
	return err
}

func (s *Service) writeStatelessWithSipgo(
	ctx context.Context,
	modeCtx outboundModeContext,
	req *sip.Request,
) error {
	if req == nil {
		return errors.New("request 为空")
	}
	if !modeCtx.SignalingReady && modeCtx.Mode != "external" {
		return signalingRuntimeNotReadyError(modeCtx.SignalingNotReadyReason)
	}
	built, err := s.buildOutboundRequest(req)
	if err != nil {
		return err
	}
	setOutboundDestination(built, modeCtx)
	if modeCtx.Client != nil {
		return modeCtx.Client.WriteRequest(built)
	}
	if modeCtx.send == nil {
		return errors.New("outbound client 为空")
	}
	return modeCtx.send(ctx, built.String())
}

func (s *Service) sendByMode(operation outboundSendOperation) (*sipResponse, error) {
	if operation.Request == nil {
		return nil, errors.New("request 为空")
	}
	if operation.Timeout <= 0 && operation.Request.Method == sip.MESSAGE {
		return nil, s.writeStatelessWithSipgo(operation.Context, operation.Mode, operation.Request)
	}
	built, err := s.buildOutboundRequest(operation.Request)
	if err != nil {
		return nil, err
	}
	setOutboundDestination(built, operation.Mode)
	if operation.Mode.Client != nil {
		response, err := operation.Mode.Client.Do(operation.Context, built)
		if err != nil {
			return nil, err
		}
		return newSIPResponse(response), nil
	}
	if operation.Mode.send == nil {
		return nil, newOutboundModeResolveError(
			"missing_direct_sender", "no direct sender for %s", destinationFromContext(operation.Mode),
		)
	}
	sender := func(raw string) error { return operation.Mode.send(operation.Context, raw) }
	return s.transport.roundTripWithSender(operation.Context, built.String(), sender)
}

func setOutboundDestination(request *sip.Request, modeCtx outboundModeContext) {
	if request == nil {
		return
	}
	if destination := destinationFromContext(modeCtx); destination != "" {
		request.SetDestination(destination)
	}
}
