package client

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/emiago/sipgo"
	"github.com/emiago/sipgo/sip"
	"github.com/iniwex5/vowifi-go/internal/vowifi/voiceclient"
)

type testRequester struct {
	mu          sync.Mutex
	err         error
	waitContext bool
	requests    chan *sip.Request
	block       <-chan struct{}
	transaction sip.ClientTransaction
}

func (r *testRequester) Request(ctx context.Context, req *sip.Request) (sip.ClientTransaction, error) {
	if r.waitContext {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	r.mu.Lock()
	err := r.err
	r.mu.Unlock()
	select {
	case r.requests <- req:
	default:
	}
	if r.block != nil {
		<-r.block
	}
	return r.transaction, err
}

type testClientTransaction struct {
	responses chan *sip.Response
	done      chan struct{}
}

func (t *testClientTransaction) Terminate()                         {}
func (t *testClientTransaction) OnTerminate(sip.FnTxTerminate) bool { return true }
func (t *testClientTransaction) Done() <-chan struct{}              { return t.done }
func (t *testClientTransaction) Err() error                         { return nil }
func (t *testClientTransaction) Responses() <-chan *sip.Response    { return t.responses }
func (t *testClientTransaction) OnRetransmission(sip.FnTxResponse) bool {
	return true
}

type testAdapter struct {
	client      *sipgo.Client
	ua          *sipgo.UserAgent
	contact     [3]string
	externalIP  string
	listenAddr  string
	contactID   string
	push        [4]string
	pushErr     error
	deviceReady chan struct{}
}

var _ voiceclient.Adapter = (*testAdapter)(nil)

type recoveredBridgeContract interface {
	Contact([]string) (string, string, string, error)
	ListenHostPort() (string, int)
	LocalIP() string
	SendPush(string, string, string, string) error
	Start(context.Context)
	StartTransaction(context.Context, string, *sip.Request) (sip.ClientTransaction, error)
	Stop()
	WriteRequest(context.Context, string, *sip.Request) error
}

var (
	_ recoveredBridgeContract                   = (*Bridge)(nil)
	_ func(string, voiceclient.Adapter) *Bridge = NewBridge
)

func (a *testAdapter) GetClient() *sipgo.Client { return a.client }
func (a *testAdapter) GetClientContact(deviceID string) (string, string, string, error) {
	a.contactID = deviceID
	return a.contact[0], a.contact[1], a.contact[2], nil
}
func (a *testAdapter) GetExternalIP() string    { return a.externalIP }
func (a *testAdapter) GetListenAddr() string    { return a.listenAddr }
func (a *testAdapter) GetUA() *sipgo.UserAgent  { return a.ua }
func (a *testAdapter) RTPPortRange() (int, int) { return 10000, 20000 }
func (a *testAdapter) SubscribeDeviceOnline(string) <-chan struct{} {
	return a.deviceReady
}
func (a *testAdapter) SendPushNotification(title, body, category, callID string) error {
	a.push = [4]string{title, body, category, callID}
	return a.pushErr
}

func newTestBridge(t *testing.T, requester *testRequester) (*Bridge, *testAdapter) {
	t.Helper()
	ua, err := sipgo.NewUA(sipgo.WithUserAgent("voice-client-test"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ua.Close() })
	client, err := sipgo.NewClient(ua)
	if err != nil {
		t.Fatal(err)
	}
	client.TxRequester = requester
	adapter := &testAdapter{client: client, ua: ua, deviceReady: make(chan struct{})}
	return NewBridge("device-29", adapter), adapter
}

func testRequest() *sip.Request {
	return sip.NewRequest(sip.INVITE, sip.Uri{Scheme: "sip", User: "alice", Host: "127.0.0.1"})
}

func TestBridgeRecoveredLayout(t *testing.T) {
	typeOfBridge := reflect.TypeOf(Bridge{})
	want := []string{"deviceID", "adapter", "client", "ua", "mu", "ctx", "cancel", "writeCh", "wg"}
	if typeOfBridge.NumField() != len(want) {
		t.Fatalf("Bridge fields = %d, want %d", typeOfBridge.NumField(), len(want))
	}
	for index, name := range want {
		if got := typeOfBridge.Field(index).Name; got != name {
			t.Fatalf("Bridge field %d = %s, want %s", index, got, name)
		}
	}
	typeOfTask := reflect.TypeOf(writeTask{})
	for index, name := range []string{"flow", "req", "enqueuedAt", "done"} {
		if got := typeOfTask.Field(index).Name; got != name {
			t.Fatalf("writeTask field %d = %s, want %s", index, got, name)
		}
	}
}

func TestBridgeDelegatesAdapterMetadata(t *testing.T) {
	bridge, adapter := newTestBridge(t, &testRequester{requests: make(chan *sip.Request, 1)})
	adapter.contact = [3]string{"sip:client@example.test", "client", "example.test"}
	adapter.externalIP = "198.51.100.29"
	adapter.listenAddr = "0.0.0.0:5090"
	first, second, third, err := bridge.Contact([]string{"ignored"})
	if err != nil || [3]string{first, second, third} != adapter.contact {
		t.Fatalf("Contact = %q, %q, %q, %v", first, second, third, err)
	}
	if adapter.contactID != "device-29" {
		t.Fatalf("contact device = %q", adapter.contactID)
	}
	if got := bridge.LocalIP(); got != adapter.externalIP {
		t.Fatalf("LocalIP = %q", got)
	}
	if host, port := bridge.ListenHostPort(); host != adapter.externalIP || port != 5090 {
		t.Fatalf("ListenHostPort = %q, %d", host, port)
	}
	if err := bridge.SendPush("title", "body", "incoming", "call-29"); err != nil {
		t.Fatal(err)
	}
	if adapter.push != [4]string{"title", "body", "incoming", "call-29"} {
		t.Fatalf("push = %#v", adapter.push)
	}
}

func TestBridgeWriteRequestReturnsWorkerResult(t *testing.T) {
	requester := &testRequester{requests: make(chan *sip.Request, 1)}
	bridge, _ := newTestBridge(t, requester)
	bridge.Start(context.Background())
	defer bridge.Stop()
	req := testRequest()
	if err := bridge.WriteRequest(context.Background(), "", req); err != nil {
		t.Fatal(err)
	}
	select {
	case got := <-requester.requests:
		if got != req {
			t.Fatal("worker did not preserve request ownership")
		}
	case <-time.After(time.Second):
		t.Fatal("sipgo requester did not receive request")
	}
	want := errors.New("write failed")
	requester.mu.Lock()
	requester.err = want
	requester.mu.Unlock()
	if err := bridge.WriteRequest(context.Background(), "failure", testRequest()); !errors.Is(err, want) {
		t.Fatalf("WriteRequest error = %v", err)
	}
}

func TestBridgeRunsFourWriteWorkers(t *testing.T) {
	release := make(chan struct{})
	requester := &testRequester{
		requests: make(chan *sip.Request, writeWorkerCount),
		block:    release,
	}
	bridge, _ := newTestBridge(t, requester)
	bridge.Start(context.Background())
	defer bridge.Stop()
	var writes sync.WaitGroup
	writes.Add(writeWorkerCount)
	for index := 0; index < writeWorkerCount; index++ {
		go func() {
			defer writes.Done()
			if err := bridge.WriteRequest(context.Background(), "parallel", testRequest()); err != nil {
				t.Errorf("WriteRequest: %v", err)
			}
		}()
	}
	for index := 0; index < writeWorkerCount; index++ {
		select {
		case <-requester.requests:
		case <-time.After(time.Second):
			t.Fatalf("only %d workers started", index)
		}
	}
	close(release)
	writes.Wait()
}

func TestBridgeWriteRequestUsesRecoveredTwoSecondTimeout(t *testing.T) {
	release := make(chan struct{})
	requester := &testRequester{requests: make(chan *sip.Request, 1), block: release}
	bridge, _ := newTestBridge(t, requester)
	bridge.Start(context.Background())
	startedAt := time.Now()
	err := bridge.WriteRequest(context.Background(), "blocked", testRequest())
	elapsed := time.Since(startedAt)
	close(release)
	bridge.Stop()
	if !errors.Is(err, ErrVoiceClientSendTimeout) {
		t.Fatalf("timeout error = %v", err)
	}
	if elapsed < 1900*time.Millisecond || elapsed > 3*time.Second {
		t.Fatalf("timeout elapsed = %v", elapsed)
	}
	if !strings.Contains(err.Error(), "flow=blocked line=INVITE sip:alice@127.0.0.1 SIP/2.0") {
		t.Fatalf("timeout details = %v", err)
	}
}

func TestBridgeWriteRequestQueueAndContextErrors(t *testing.T) {
	bridge, _ := newTestBridge(t, &testRequester{requests: make(chan *sip.Request, 1)})
	if err := bridge.WriteRequest(context.Background(), "flow", testRequest()); !errors.Is(err, errWriteQueueUninitialized) {
		t.Fatalf("unstarted error = %v", err)
	}
	bridge.mu.Lock()
	bridge.ctx = context.Background()
	bridge.writeCh = make(chan writeTask, writeQueueSize)
	for index := 0; index < writeQueueSize; index++ {
		bridge.writeCh <- writeTask{}
	}
	bridge.mu.Unlock()
	if err := bridge.WriteRequest(context.Background(), "full", testRequest()); !errors.Is(err, ErrVoiceClientWriteQueueFull) {
		t.Fatalf("full queue error = %v", err)
	}
	bridge.mu.Lock()
	bridge.writeCh = nil
	bridge.ctx = nil
	bridge.mu.Unlock()
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	bridge.Start(canceled)
	defer bridge.Stop()
	if err := bridge.WriteRequest(nil, "canceled", testRequest()); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled error = %v", err)
	}
}

func TestBridgeStartTransactionAndDeadlineClassification(t *testing.T) {
	wantTransaction := &testClientTransaction{
		responses: make(chan *sip.Response, 1),
		done:      make(chan struct{}),
	}
	requester := &testRequester{
		requests:    make(chan *sip.Request, 1),
		transaction: wantTransaction,
	}
	bridge, _ := newTestBridge(t, requester)
	transaction, err := bridge.StartTransaction(context.Background(), "", testRequest())
	if err != nil || transaction != wantTransaction {
		t.Fatalf("transaction = %v, err = %v", transaction, err)
	}
	wantResponse := sip.NewResponse(180, "Ringing")
	wantTransaction.responses <- wantResponse
	if response := <-transaction.Responses(); response != wantResponse {
		t.Fatal("bridge replaced the transaction response channel")
	}
	requester.waitContext = true
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	if _, err := bridge.StartTransaction(ctx, "deadline", testRequest()); !errors.Is(err, ErrVoiceClientTransactionTimeout) {
		t.Fatalf("deadline error = %v", err)
	}
}

func TestBridgeStopAllowsRestart(t *testing.T) {
	bridge, _ := newTestBridge(t, &testRequester{requests: make(chan *sip.Request, 4)})
	for iteration := 0; iteration < 10; iteration++ {
		bridge.Start(context.Background())
		if err := bridge.WriteRequest(context.Background(), "restart", testRequest()); err != nil {
			t.Fatal(err)
		}
		bridge.Stop()
	}
}

func TestBridgeRecoveredErrors(t *testing.T) {
	bridge := NewBridge("device-29", nil)
	if _, _, _, err := bridge.Contact(nil); err == nil || err.Error() != "voice client adapter 未初始化" {
		t.Fatalf("Contact error = %v", err)
	}
	if err := bridge.WriteRequest(context.Background(), "", nil); err == nil || err.Error() != "client SIP request 为空" {
		t.Fatalf("WriteRequest error = %v", err)
	}
	if _, err := bridge.StartTransaction(context.Background(), "", nil); err == nil || err.Error() != "client transaction request 为空" {
		t.Fatalf("StartTransaction error = %v", err)
	}
}
