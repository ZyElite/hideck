package voice

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/emiago/sipgo"
	"github.com/emiago/sipgo/sip"
	"github.com/iniwex5/vowifi-go/internal/vowifi/voiceclient"
)

type agentClientRequester struct {
	requests chan *sip.Request
}

func (r *agentClientRequester) Request(_ context.Context, req *sip.Request) (sip.ClientTransaction, error) {
	r.requests <- req
	return nil, nil
}

type agentVoiceClientAdapter struct {
	client *sipgo.Client
	ua     *sipgo.UserAgent
}

var _ voiceclient.Adapter = (*agentVoiceClientAdapter)(nil)

func (a *agentVoiceClientAdapter) GetClient() *sipgo.Client { return a.client }
func (a *agentVoiceClientAdapter) GetClientContact(string) (string, string, string, error) {
	return "sip:client@127.0.0.1", "client", "127.0.0.1", nil
}
func (a *agentVoiceClientAdapter) GetExternalIP() string    { return "127.0.0.1" }
func (a *agentVoiceClientAdapter) GetListenAddr() string    { return "127.0.0.1:5060" }
func (a *agentVoiceClientAdapter) GetUA() *sipgo.UserAgent  { return a.ua }
func (a *agentVoiceClientAdapter) RTPPortRange() (int, int) { return 10000, 20000 }
func (a *agentVoiceClientAdapter) SendPushNotification(string, string, string, string) error {
	return nil
}
func (a *agentVoiceClientAdapter) SubscribeDeviceOnline(string) <-chan struct{} {
	return make(chan struct{})
}

func TestGatewayStartsAndStopsProductionClientBridge(t *testing.T) {
	ua, err := sipgo.NewUA(sipgo.WithUserAgent("voice-agent-client-test"))
	if err != nil {
		t.Fatal(err)
	}
	defer ua.Close()
	client, err := sipgo.NewClient(ua)
	if err != nil {
		t.Fatal(err)
	}
	requester := &agentClientRequester{requests: make(chan *sip.Request, 1)}
	client.TxRequester = requester
	adapter := &agentVoiceClientAdapter{client: client, ua: ua}
	agent := NewAgent("client-production-device", nil, nil)
	gateway := NewGateway(agent)
	gateway.SetClientAdapter(adapter)
	if gateway.GetClientAdapter() != adapter || agent.GetClientAdapter() != adapter {
		t.Fatal("gateway did not project the adapter into the agent")
	}
	if err := gateway.StartCurrent(); err != nil {
		t.Fatal(err)
	}
	req := sip.NewRequest(sip.OPTIONS, sip.Uri{Scheme: "sip", User: "client", Host: "127.0.0.1"})
	if err := agent.clientBridge.WriteRequest(context.Background(), "agent_production", req); err != nil {
		t.Fatal(err)
	}
	select {
	case got := <-requester.requests:
		if got != req {
			t.Fatal("agent bridge changed the structured request")
		}
	case <-time.After(time.Second):
		t.Fatal("agent production bridge did not call sipgo client")
	}
	if err := gateway.Stop(); err != nil {
		t.Fatal(err)
	}
	err = agent.clientBridge.WriteRequest(context.Background(), "after_stop", req)
	if err == nil || !strings.Contains(err.Error(), "写队列未初始化") {
		t.Fatalf("post-stop write error = %v", err)
	}
}

func TestAgentSerializesClientAdapterReplacementWithStop(t *testing.T) {
	ua, err := sipgo.NewUA(sipgo.WithUserAgent("voice-agent-client-race-test"))
	if err != nil {
		t.Fatal(err)
	}
	defer ua.Close()
	client, err := sipgo.NewClient(ua)
	if err != nil {
		t.Fatal(err)
	}
	client.TxRequester = &agentClientRequester{requests: make(chan *sip.Request, 64)}
	adapter := &agentVoiceClientAdapter{client: client, ua: ua}
	agent := NewAgent("client-race-device", nil, nil)
	agent.SetClientAdapter(adapter)
	if err := agent.StartCurrent(); err != nil {
		t.Fatal(err)
	}
	var operations sync.WaitGroup
	operations.Add(2)
	go func() {
		defer operations.Done()
		for iteration := 0; iteration < 25; iteration++ {
			agent.SetClientAdapter(adapter)
		}
	}()
	go func() {
		defer operations.Done()
		if err := agent.Stop(); err != nil {
			t.Errorf("Stop: %v", err)
		}
	}()
	operations.Wait()
	err = agent.clientBridge.WriteRequest(context.Background(), "after_stop", testAgentClientRequest())
	if err == nil || !strings.Contains(err.Error(), "写队列未初始化") {
		t.Fatalf("post-stop write error = %v", err)
	}
}

func testAgentClientRequest() *sip.Request {
	return sip.NewRequest(sip.OPTIONS, sip.Uri{Scheme: "sip", User: "client", Host: "127.0.0.1"})
}
