package voice

import (
	"github.com/emiago/sipgo/sip"
	"github.com/iniwex5/vowifi-go/internal/vowifi/imscore"
	"github.com/iniwex5/vowifi-go/internal/vowifi/imsendpoint"
	"github.com/iniwex5/vowifi-go/internal/vowifi/voice/callstate"
	"github.com/iniwex5/vowifi-go/internal/vowifi/voice/client"
)

func (c *Call) setServerInvite(handle imsendpoint.ServerInviteHandle, request *sip.Request) {
	c.mu.Lock()
	c.imsServerInvite = handle
	if request != nil {
		c.imsInviteRequest = request.Clone()
	} else {
		c.imsInviteRequest = nil
	}
	c.mu.Unlock()
}

func (c *Call) hasServerInvite() bool {
	if c == nil {
		return false
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.imsServerInvite != nil && c.imsInviteRequest != nil
}

func (c *Call) markInboundPrepared() {
	if c == nil {
		return
	}
	c.mu.Lock()
	c.inboundPrepared = true
	c.mu.Unlock()
}

func (c *Call) startInboundClientOnce() bool {
	if c == nil {
		return false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.inboundPrepared || c.inboundClientStarted || callstate.IsTerminal(callstate.State(c.State)) {
		return false
	}
	c.inboundClientStarted = true
	return true
}

type clientRequestContext struct {
	request     *sip.Request
	destination string
	localIP     string
	fromTag     string
}

func (c *Call) storeClientRequestContext(value clientRequestContext) {
	c.mu.Lock()
	c.clientInviteRequest = value.request.Clone()
	c.DialogState.ClientDest = value.destination
	c.DialogState.ClientLocalIP = value.localIP
	c.DialogState.ClientFromTag = value.fromTag
	c.mu.Unlock()
}

func (c *Call) setInboundClientBridge(bridge *client.Bridge) {
	if c == nil {
		return
	}
	c.mu.Lock()
	c.inboundClientBridge = bridge
	c.mu.Unlock()
}

func (c *Call) inboundBridge() *client.Bridge {
	if c == nil {
		return nil
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.inboundClientBridge
}

func (c *Call) storeClientInvite(request *sip.Request) {
	if c == nil || request == nil {
		return
	}
	c.mu.Lock()
	c.clientInviteRequest = request.Clone()
	c.mu.Unlock()
}

func (c *Call) storeClientInviteResponse(response *sip.Response) {
	if c == nil || response == nil {
		return
	}
	c.mu.Lock()
	c.clientInviteResponse = response.Clone()
	c.DialogState.ClientToTag = sipHeaderTag(response.To())
	c.mu.Unlock()
}

func (c *Call) clientDialogContext() (*sip.Request, *sip.Response) {
	if c == nil {
		return nil, nil
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	var request *sip.Request
	var response *sip.Response
	if c.clientInviteRequest != nil {
		request = c.clientInviteRequest.Clone()
	}
	if c.clientInviteResponse != nil {
		response = c.clientInviteResponse.Clone()
	}
	return request, response
}

func (c *Call) takeClientCancelRequest() *sip.Request {
	if c == nil {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.clientCancelSent || c.clientInviteRequest == nil {
		return nil
	}
	c.clientCancelSent = true
	return c.clientInviteRequest.Clone()
}

func (c *Call) takeClientByeContext() (*sip.Request, *sip.Response) {
	if c == nil {
		return nil, nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.clientByeSent || c.clientInviteRequest == nil || c.clientInviteResponse == nil {
		return nil, nil
	}
	c.clientByeSent = true
	return c.clientInviteRequest.Clone(), c.clientInviteResponse.Clone()
}

func (c *Call) serverInviteContext() (imsendpoint.ServerInviteHandle, *sip.Request) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	request := c.imsInviteRequest
	if request != nil {
		request = request.Clone()
	}
	return c.imsServerInvite, request
}

func (c *Call) setInboundRequest(responder imscore.InboundVoiceResponder) {
	c.mu.Lock()
	c.inboundResponder = responder
	c.mu.Unlock()
}

func (c *Call) inboundResponseWriter() imscore.InboundVoiceResponder {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.inboundResponder
}

func (c *Call) inboundLocalTagValue() string {
	if c == nil {
		return ""
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.DialogState.ToTag
}

func (c *Call) setRemoteSDP(remote, clientRemote string) {
	c.mu.Lock()
	c.remoteSDP = remote
	c.clientRemoteSDP = clientRemote
	c.mu.Unlock()
}

func (c *Call) setLocalSDP(client, ims string) {
	c.mu.Lock()
	c.clientLocalSDP = client
	c.imsLocalSDP = ims
	c.mu.Unlock()
}

func (c *Call) remoteSDPValue() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.remoteSDP
}

func (c *Call) clientRemoteSDPValue() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.clientRemoteSDP
}

func (c *Call) localSDPs() (string, string) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.clientLocalSDP, c.imsLocalSDP
}

func (c *Call) incomingSnapshot() IncomingCall {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return IncomingCall{
		DeviceID: c.agent.DeviceID(), CallID: c.callID, Caller: c.peer,
		Callee: c.callee, OfferSDP: c.clientRemoteSDP,
		ReceivedAt: c.startTime, State: callstate.State(c.State).String(),
	}
}

// ClientSDP returns the latest SDP that the local client must consume.
func (c *Call) ClientSDP() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.clientRemoteSDP
}

func (c *Call) imsLocalSDPValue() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.imsLocalSDP
}
