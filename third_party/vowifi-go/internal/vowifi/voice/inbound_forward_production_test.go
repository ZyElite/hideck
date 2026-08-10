package voice

import (
	"context"
	"fmt"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/emiago/sipgo"
	"github.com/emiago/sipgo/sip"
	"github.com/iniwex5/vowifi-go/internal/vowifi/imsendpoint"
	"github.com/iniwex5/vowifi-go/internal/vowifi/voiceclient"
)

type inboundLocalTransaction struct {
	responses chan *sip.Response
	done      chan struct{}
	once      sync.Once
}

func (t *inboundLocalTransaction) Terminate() {
	t.once.Do(func() { close(t.done) })
}

func (t *inboundLocalTransaction) OnTerminate(sip.FnTxTerminate) bool { return true }
func (t *inboundLocalTransaction) Done() <-chan struct{}              { return t.done }
func (t *inboundLocalTransaction) Err() error                         { return nil }
func (t *inboundLocalTransaction) Responses() <-chan *sip.Response    { return t.responses }
func (t *inboundLocalTransaction) OnRetransmission(sip.FnTxResponse) bool {
	return true
}

type inboundLocalRequest struct {
	request     *sip.Request
	transaction *inboundLocalTransaction
}

type inboundLocalRequester struct {
	requests chan inboundLocalRequest
}

func (r *inboundLocalRequester) Request(
	_ context.Context,
	request *sip.Request,
) (sip.ClientTransaction, error) {
	captured := inboundLocalRequest{request: request.Clone()}
	if request.Method == sip.INVITE {
		captured.transaction = &inboundLocalTransaction{
			responses: make(chan *sip.Response, 4), done: make(chan struct{}),
		}
	}
	r.requests <- captured
	return captured.transaction, nil
}

type inboundLocalAdapter struct {
	client *sipgo.Client
	ua     *sipgo.UserAgent
	online chan struct{}
}

var _ voiceclient.Adapter = (*inboundLocalAdapter)(nil)

func (a *inboundLocalAdapter) GetClient() *sipgo.Client { return a.client }
func (a *inboundLocalAdapter) GetClientContact(string) (string, string, string, error) {
	return "sip:placeholder@127.0.0.1:5072", "127.0.0.1:5072", "client", nil
}
func (a *inboundLocalAdapter) GetExternalIP() string    { return "127.0.0.1" }
func (a *inboundLocalAdapter) GetListenAddr() string    { return "127.0.0.1:5070" }
func (a *inboundLocalAdapter) GetUA() *sipgo.UserAgent  { return a.ua }
func (a *inboundLocalAdapter) RTPPortRange() (int, int) { return 10000, 20000 }
func (a *inboundLocalAdapter) SendPushNotification(string, string, string, string) error {
	return nil
}
func (a *inboundLocalAdapter) SubscribeDeviceOnline(string) <-chan struct{} { return a.online }

type originalInboundAgentAPI interface {
	OnIMSInvite(*sip.Request, *imsendpoint.Session, imsendpoint.ServerInviteHandle)
	HandleIMSByeEvent(imsendpoint.Event)
	HandleIMSCancelEvent(imsendpoint.Event)
	HandleIMSUpdateEvent(imsendpoint.Event)
}

type originalInboundGatewayAPI interface {
	OnIMSInvite(string, []byte, *imsendpoint.Session)
}

var _ originalInboundAgentAPI = (*Agent)(nil)
var _ originalInboundGatewayAPI = (*Gateway)(nil)

func TestInboundNetworkCallForwardsToLocalClientAndClosesBothDialogs(t *testing.T) {
	registrar := startInboundDialogRegistrar(t)
	agent := newVoiceTestAgent(t, registrar.conn)
	requester := installInboundLocalAdapter(t, agent)
	if err := agent.StartCurrent(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = agent.Stop() })

	clientAddr := registrar.waitClient(t)
	imsPeer := listenVoiceUDP(t)
	clientPeer := listenVoiceUDP(t)
	callID := "inbound-local-forward"
	sendInboundWireRequest(t, registrar, clientAddr,
		inboundNetworkInvite(registrar.conn.LocalAddr().String(), callID,
			voiceSDP(imsPeer.LocalAddr().(*net.UDPAddr).Port)))
	_ = registrar.waitStatus(t, 100)
	provisional := registrar.waitStatus(t, 180)

	invite := waitInboundLocalRequest(t, requester, sip.INVITE)
	assertInboundLocalInvite(t, invite.request, callID)
	response := sip.NewResponseFromRequest(invite.request, 200, "OK",
		[]byte(voiceSDP(clientPeer.LocalAddr().(*net.UDPAddr).Port)))
	response.To().Params.Add("tag", "local-client")
	response.AppendHeader(&sip.ContactHeader{Address: sip.Uri{
		Scheme: "sip", User: "client", Host: "127.0.0.1", Port: 5072,
	}})
	response.AppendHeader(sip.NewHeader("Content-Type", "application/sdp"))
	invite.transaction.responses <- response

	ack := waitInboundLocalRequest(t, requester, sip.ACK)
	if ack.request.CSeq() == nil || ack.request.CSeq().SeqNo != 1 {
		t.Fatalf("local ACK CSeq = %#v", ack.request.CSeq())
	}
	accepted := registrar.waitStatus(t, 200)
	if voiceHeaderTag(voiceTestHeader(accepted, "To")) !=
		voiceHeaderTag(voiceTestHeader(provisional, "To")) {
		t.Fatal("IMS provisional and accepted responses used different To tags")
	}
	sendInboundWireRequest(t, registrar, clientAddr,
		inboundNetworkACK(registrar.conn.LocalAddr().String(), callID, voiceTestHeader(accepted, "To")))
	sendInboundWireRequest(t, registrar, clientAddr,
		inboundNetworkBYE(registrar.conn.LocalAddr().String(), callID, voiceTestHeader(accepted, "To")))
	_ = registrar.waitResponse(t, func(raw string) bool {
		return strings.HasPrefix(raw, "SIP/2.0 200 ") &&
			strings.HasSuffix(voiceTestHeader(raw, "CSeq"), " BYE")
	}, "BYE 200")
	bye := waitInboundLocalRequest(t, requester, sip.BYE)
	if bye.request.CSeq() == nil || bye.request.CSeq().SeqNo != 2 {
		t.Fatalf("local BYE CSeq = %#v", bye.request.CSeq())
	}
	waitForAgentIdle(t, agent)
}

func TestInboundNetworkCancelForwardsToLocalClient(t *testing.T) {
	registrar := startInboundDialogRegistrar(t)
	agent := newVoiceTestAgent(t, registrar.conn)
	requester := installInboundLocalAdapter(t, agent)
	if err := agent.StartCurrent(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = agent.Stop() })

	clientAddr := registrar.waitClient(t)
	imsPeer := listenVoiceUDP(t)
	callID := "inbound-local-cancel"
	sendInboundWireRequest(t, registrar, clientAddr,
		inboundNetworkInvite(registrar.conn.LocalAddr().String(), callID,
			voiceSDP(imsPeer.LocalAddr().(*net.UDPAddr).Port)))
	_ = registrar.waitStatus(t, 100)
	provisional := registrar.waitStatus(t, 180)
	invite := waitInboundLocalRequest(t, requester, sip.INVITE)

	sendInboundWireRequest(t, registrar, clientAddr,
		inboundNetworkCancel(registrar.conn.LocalAddr().String(), callID, voiceTestHeader(provisional, "To")))
	cancel := waitInboundLocalRequest(t, requester, sip.CANCEL)
	if cancel.request.CSeq() == nil || invite.request.CSeq() == nil ||
		cancel.request.CSeq().SeqNo != invite.request.CSeq().SeqNo {
		t.Fatalf("local CANCEL CSeq=%#v INVITE CSeq=%#v", cancel.request.CSeq(), invite.request.CSeq())
	}
	_ = registrar.waitResponse(t, func(raw string) bool {
		return strings.HasPrefix(raw, "SIP/2.0 200 ") &&
			strings.HasSuffix(voiceTestHeader(raw, "CSeq"), " CANCEL")
	}, "CANCEL 200")
	_ = registrar.waitStatus(t, 487)
	waitForAgentIdle(t, agent)
}

func installInboundLocalAdapter(t *testing.T, agent *Agent) *inboundLocalRequester {
	t.Helper()
	ua, err := sipgo.NewUA(sipgo.WithUserAgent("inbound-local-test"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ua.Close() })
	client, err := sipgo.NewClient(ua)
	if err != nil {
		t.Fatal(err)
	}
	requester := &inboundLocalRequester{requests: make(chan inboundLocalRequest, 8)}
	client.TxRequester = requester
	agent.SetClientAdapter(&inboundLocalAdapter{
		client: client, ua: ua, online: make(chan struct{}),
	})
	return requester
}

func waitInboundLocalRequest(
	t *testing.T,
	requester *inboundLocalRequester,
	method sip.RequestMethod,
) inboundLocalRequest {
	t.Helper()
	deadline := time.After(2 * time.Second)
	for {
		select {
		case request := <-requester.requests:
			if request.request != nil && request.request.Method == method {
				return request
			}
		case <-deadline:
			t.Fatalf("local client did not receive %s", method)
		}
	}
}

func assertInboundLocalInvite(t *testing.T, request *sip.Request, imsCallID string) {
	t.Helper()
	if request == nil || request.Method != sip.INVITE {
		t.Fatalf("local INVITE = %#v", request)
	}
	if request.Recipient.User != "client" || request.Destination() != "127.0.0.1:5072" {
		t.Fatalf("local target = %s destination=%q", request.Recipient.String(), request.Destination())
	}
	if request.CallID() == nil || !strings.HasPrefix(request.CallID().Value(), imsCallID+"-") {
		t.Fatalf("local Call-ID = %#v", request.CallID())
	}
	if request.ContentType() == nil || request.ContentType().Value() != "application/sdp" || len(request.Body()) == 0 {
		t.Fatal("local INVITE is missing the relayed SDP offer")
	}
}

func sendInboundWireRequest(
	t *testing.T,
	registrar *inboundDialogRegistrar,
	destination *net.UDPAddr,
	request string,
) {
	t.Helper()
	if _, err := registrar.conn.WriteToUDP([]byte(request), destination); err != nil {
		t.Fatal(err)
	}
}

func inboundNetworkBYE(localAddr, callID, to string) string {
	return fmt.Sprintf(
		"BYE sip:user@ims.example.com SIP/2.0\r\nVia: SIP/2.0/UDP %s;rport;branch=z9hG4bK-bye-28\r\nFrom: <sip:+447700900123@ims.example.com>;tag=remote-28\r\nTo: %s\r\nCall-ID: %s\r\nCSeq: 2 BYE\r\nContent-Length: 0\r\n\r\n",
		localAddr, to, callID,
	)
}
