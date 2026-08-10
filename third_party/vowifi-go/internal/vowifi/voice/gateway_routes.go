package voice

import (
	"context"
	"errors"
	"strings"

	"github.com/emiago/sipgo/sip"
	"github.com/iniwex5/vowifi-go/internal/vowifi/imsendpoint"
	"github.com/iniwex5/vowifi-go/internal/vowifi/logging"
)

// OnIMSInvite parses and serializes a raw endpoint INVITE for one device.
func (g *Gateway) OnIMSInvite(deviceID string, raw []byte, session *imsendpoint.Session) {
	request := g.parseRawIMSRequest(deviceID, raw, "INVITE")
	if request == nil {
		return
	}
	if !g.enqueueDeviceTask(deviceID, "ims_invite", func(agent *Agent) {
		agent.OnIMSInvite(request, session, nil)
	}) {
		g.warnRejectedRoute(deviceID, "INVITE")
	}
}

// OnIMSBye parses and serializes a raw endpoint BYE for one device.
func (g *Gateway) OnIMSBye(deviceID string, raw []byte, session *imsendpoint.Session) {
	g.enqueueIMSRequest(imsRequestRoute{
		deviceID: deviceID, raw: raw, session: session,
		method: "BYE", handler: (*Agent).HandleIMSByeEvent,
	})
}

// OnIMSUpdate parses and serializes a raw endpoint UPDATE for one device.
func (g *Gateway) OnIMSUpdate(deviceID string, raw []byte, session *imsendpoint.Session) {
	g.enqueueIMSRequest(imsRequestRoute{
		deviceID: deviceID, raw: raw, session: session,
		method: "UPDATE", handler: (*Agent).HandleIMSUpdateEvent,
	})
}

// OnIMSCancel parses and serializes a raw endpoint CANCEL for one device.
func (g *Gateway) OnIMSCancel(deviceID string, raw []byte, session *imsendpoint.Session) {
	g.enqueueIMSRequest(imsRequestRoute{
		deviceID: deviceID, raw: raw, session: session,
		method: "CANCEL", handler: (*Agent).HandleIMSCancelEvent,
	})
}

type imsRequestRoute struct {
	deviceID string
	raw      []byte
	session  *imsendpoint.Session
	method   string
	handler  func(*Agent, imsendpoint.Event)
}

func (g *Gateway) enqueueIMSRequest(route imsRequestRoute) {
	request := g.parseRawIMSRequest(route.deviceID, route.raw, route.method)
	if request == nil {
		return
	}
	event := imsendpoint.Event{
		DeviceID: route.deviceID, Kind: "request", Method: route.method,
		CallID: requestCallID(request), Request: request, Session: route.session,
	}
	if !g.enqueueDeviceTask(route.deviceID, "ims_"+strings.ToLower(route.method), func(agent *Agent) {
		route.handler(agent, event)
	}) {
		g.warnRejectedRoute(route.deviceID, route.method)
	}
}

func (g *Gateway) parseRawIMSRequest(deviceID string, raw []byte, method string) *sip.Request {
	request, err := parseVoiceRequest(string(raw))
	if err != nil {
		logging.WarnRate("voice-gateway-parse:"+deviceID+":"+method,
			voiceActorEventLogInterval, "voice gateway request parse failed",
			"device", deviceID, "method", method, "err", err)
		return nil
	}
	return request
}

func (g *Gateway) warnRejectedRoute(deviceID, method string) {
	logging.WarnRate("voice-gateway-route:"+deviceID+":"+method,
		voiceActorEventLogInterval, "voice gateway rejected device task",
		"device", deviceID, "method", method)
}

// HandleClientInvite routes a local INVITE to its device Agent.
func (g *Gateway) HandleClientInvite(
	deviceID string,
	request *sip.Request,
	transaction sip.ServerTransaction,
) {
	g.enqueueClientTransaction(clientTransactionRoute{
		deviceID: deviceID, taskName: "client_invite", request: request, transaction: transaction,
		missingStatus: 500, missingReason: "Internal Server Error - Agent Not Found",
		queueReason: "Service Unavailable - Queue Full", handler: (*Agent).HandleOutboundInvite,
	})
}

// HandleClientCancel routes a local CANCEL to its device Agent.
func (g *Gateway) HandleClientCancel(
	deviceID string,
	request *sip.Request,
	transaction sip.ServerTransaction,
) {
	g.enqueueClientTransaction(clientTransactionRoute{
		deviceID: deviceID, taskName: "client_cancel", request: request, transaction: transaction,
		missingStatus: 481, missingReason: "Call/Transaction Does Not Exist",
		queueReason: "Service Unavailable - Queue Full", handler: (*Agent).HandleCancel,
	})
}

// HandleClientPrack routes a local PRACK to its device Agent.
func (g *Gateway) HandleClientPrack(
	deviceID string,
	request *sip.Request,
	transaction sip.ServerTransaction,
) {
	g.enqueueClientTransaction(clientTransactionRoute{
		deviceID: deviceID, taskName: "client_prack", request: request, transaction: transaction,
		missingStatus: 481, missingReason: "Call/Transaction Does Not Exist",
		queueReason: "Service Unavailable - Queue Full", handler: (*Agent).HandlePrack,
	})
}

// HandleClientBye routes a local BYE to its device Agent.
func (g *Gateway) HandleClientBye(
	deviceID string,
	request *sip.Request,
	transaction sip.ServerTransaction,
) {
	deviceID = strings.TrimSpace(deviceID)
	if g.GetAgent(deviceID) == nil {
		return
	}
	if !g.enqueueDeviceTask(deviceID, "client_bye", func(agent *Agent) {
		agent.HandleClientBye(request, transaction)
	}) {
		g.warnRejectedRoute(deviceID, "BYE")
	}
}

type clientTransactionRoute struct {
	deviceID      string
	taskName      string
	request       *sip.Request
	transaction   sip.ServerTransaction
	missingStatus int
	missingReason string
	queueReason   string
	handler       func(*Agent, *sip.Request, sip.ServerTransaction)
}

func (g *Gateway) enqueueClientTransaction(route clientTransactionRoute) {
	route.deviceID = strings.TrimSpace(route.deviceID)
	if g.GetAgent(route.deviceID) == nil {
		_ = respondClientRequest(route.transaction, route.request, route.missingStatus, route.missingReason)
		return
	}
	if !g.enqueueDeviceTask(route.deviceID, route.taskName, func(agent *Agent) {
		route.handler(agent, route.request, route.transaction)
	}) {
		_ = respondClientRequest(route.transaction, route.request, 503, route.queueReason)
	}
}

// HandleClientAck routes an ACK without generating a SIP response.
func (g *Gateway) HandleClientAck(deviceID string, request *sip.Request) {
	deviceID = strings.TrimSpace(deviceID)
	if !g.enqueueDeviceTask(deviceID, "client_ack", func(agent *Agent) {
		agent.HandleOutboundACK(request)
	}) {
		g.warnRejectedRoute(deviceID, "ACK")
	}
}

// SimulateCall runs the recovered device-scoped timed-call workflow.
func (g *Gateway) SimulateCall(
	ctx context.Context,
	deviceID string,
	request SimulateCallRequest,
) (*SimulateCallResult, error) {
	agent := g.GetAgent(deviceID)
	if agent == nil {
		return nil, errors.New("voice: agent not found for device " + strings.TrimSpace(deviceID))
	}
	return agent.SimulateCall(ctx, request)
}

// SimulateCallNumber retains the additive direct-dial convenience API.
func (g *Gateway) SimulateCallNumber(deviceID, number string) (*Call, error) {
	agent := g.GetAgent(deviceID)
	if agent == nil {
		return nil, errors.New("voice: agent not found for device " + strings.TrimSpace(deviceID))
	}
	return agent.simulateCall(number)
}
