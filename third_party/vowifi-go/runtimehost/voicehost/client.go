package voicehost

import (
	"errors"

	"github.com/emiago/sipgo/sip"
	"github.com/iniwex5/vowifi-go/internal/vowifi/voiceclient"
)

type ClientAdapter = voiceclient.Adapter

// ClientAdapterCurrent retains the displaced string-only client boundary.
type ClientAdapterCurrent interface {
	HandleClientInvite(peer, sdp string) (interface{}, error)
	HandleClientBye(callID string) error
	HandleClientCancel(callID string) error
	HandleClientAck(callID string) error
	HandleClientPrack(callID string) error
}

func (g *Gateway) SetClientAdapter(adapter ClientAdapter) {
	if g != nil && g.inner != nil {
		g.inner.SetClientAdapter(adapter)
	}
}

func (g *Gateway) HandleClientInvite(
	deviceID string,
	request *sip.Request,
	transaction sip.ServerTransaction,
) {
	if g != nil && g.inner != nil {
		g.inner.HandleClientInvite(deviceID, request, transaction)
	}
}

func (g *Gateway) HandleClientCancel(
	deviceID string,
	request *sip.Request,
	transaction sip.ServerTransaction,
) {
	if g != nil && g.inner != nil {
		g.inner.HandleClientCancel(deviceID, request, transaction)
	}
}

func (g *Gateway) HandleClientPrack(
	deviceID string,
	request *sip.Request,
	transaction sip.ServerTransaction,
) {
	if g != nil && g.inner != nil {
		g.inner.HandleClientPrack(deviceID, request, transaction)
	}
}

func (g *Gateway) HandleClientAck(deviceID string, request *sip.Request) {
	if g != nil && g.inner != nil {
		g.inner.HandleClientAck(deviceID, request)
	}
}

func (g *Gateway) HandleClientBye(
	deviceID string,
	request *sip.Request,
	transaction sip.ServerTransaction,
) {
	if g != nil && g.inner != nil {
		g.inner.HandleClientBye(deviceID, request, transaction)
	}
}

func (g *Gateway) SetClientAdapterCurrent(adapter ClientAdapterCurrent) {
	if g == nil {
		return
	}
	g.mu.Lock()
	g.currentClient = adapter
	g.mu.Unlock()
}

func (g *Gateway) HandleClientInviteCurrent(peer, sdp string) (interface{}, error) {
	adapter, err := g.currentClientAdapter()
	if err != nil {
		return nil, err
	}
	return adapter.HandleClientInvite(peer, sdp)
}

func (g *Gateway) HandleClientByeCurrent(callID string) error {
	adapter, err := g.currentClientAdapter()
	if err != nil {
		return err
	}
	return adapter.HandleClientBye(callID)
}

func (g *Gateway) HandleClientCancelCurrent(callID string) error {
	adapter, err := g.currentClientAdapter()
	if err != nil {
		return err
	}
	return adapter.HandleClientCancel(callID)
}

func (g *Gateway) HandleClientAckCurrent(callID string) error {
	adapter, err := g.currentClientAdapter()
	if err != nil {
		return err
	}
	return adapter.HandleClientAck(callID)
}

func (g *Gateway) HandleClientPrackCurrent(callID string) error {
	adapter, err := g.currentClientAdapter()
	if err != nil {
		return err
	}
	return adapter.HandleClientPrack(callID)
}

func (g *Gateway) currentClientAdapter() (ClientAdapterCurrent, error) {
	if g == nil {
		return nil, errors.New("voicehost: no client adapter")
	}
	g.mu.RLock()
	defer g.mu.RUnlock()
	if g.currentClient == nil {
		return nil, errors.New("voicehost: no client adapter")
	}
	return g.currentClient, nil
}
