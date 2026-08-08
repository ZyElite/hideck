package voice

import (
	"github.com/iniwex5/vowifi-go/internal/vowifi/imscore"
)

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
		ReceivedAt: c.startTime, State: c.state.String(),
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
