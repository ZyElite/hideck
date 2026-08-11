package api

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/iniwex5/vohive/internal/balance"
	"github.com/iniwex5/vohive/internal/carrierquery"
	"github.com/iniwex5/vohive/internal/commandcenter"
	"github.com/iniwex5/vohive/internal/config"
	"github.com/iniwex5/vohive/internal/db"
	"github.com/iniwex5/vohive/internal/notify"
	"gorm.io/gorm"
)

type apiBalanceGateway struct {
	smsCalls int
}

func (g *apiBalanceGateway) Snapshot(deviceID string) (balance.DeviceSnapshot, error) {
	return balance.DeviceSnapshot{DeviceID: deviceID, ICCID: "iccid-1", MCC: "234", MNC: "10"}, nil
}

func (g *apiBalanceGateway) SendVoWiFiSMS(context.Context, string, string, string) error {
	return errors.New("unexpected VoWiFi route")
}

func (g *apiBalanceGateway) SendBackendSMS(context.Context, string, string, string) error {
	g.smsCalls++
	return nil
}

func (*apiBalanceGateway) SendVoWiFiUSSD(context.Context, string, string) (balance.USSDResponse, error) {
	return balance.USSDResponse{}, errors.New("unexpected VoWiFi USSD route")
}

func (*apiBalanceGateway) SendBackendUSSD(context.Context, string, string) (balance.USSDResponse, error) {
	return balance.USSDResponse{}, errors.New("unexpected backend USSD route")
}

type apiRuleResolver struct{}

func (apiRuleResolver) Resolve(context.Context, balance.DeviceSnapshot) (carrierquery.Rule, error) {
	return carrierquery.Rule{ID: "giffgaff", MCC: "234", MNC: "10", Operator: "giffgaff",
		Transport: carrierquery.TransportSMS, Destination: "85075", Payload: "INFO",
		ResponseMode: carrierquery.ResponseSMS, ExpectedSenders: []string{"giffgaff"}, Enabled: true}, nil
}

func (apiRuleResolver) ByID(context.Context, string) (carrierquery.Rule, error) {
	return apiRuleResolver{}.Resolve(context.Background(), balance.DeviceSnapshot{})
}

func TestCommandCenterRoutesRequireAuthAndPersistExecution(t *testing.T) {
	server, token, _ := newCommandCenterAPITestServer(t)
	router := server.newRouter()
	unauthorized := performAPIRequest(router, http.MethodGet, "/api/commands/catalog", "", nil)
	if unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized status = %d", unauthorized.Code)
	}
	body := bytes.NewBufferString(`{"input":"/list"}`)
	accepted := performAPIRequest(router, http.MethodPost, "/api/commands/executions", token, body)
	if accepted.Code != http.StatusAccepted {
		t.Fatalf("execute status = %d body=%s", accepted.Code, accepted.Body.String())
	}
	waitForAPIEvents(t, server.commandCenter, 2)
	events := performAPIRequest(router, http.MethodGet, "/api/commands/events?after_id=0", token, nil)
	if events.Code != http.StatusOK || !strings.Contains(events.Body.String(), `"state":"completed"`) {
		t.Fatalf("events status=%d body=%s", events.Code, events.Body.String())
	}
}

func TestBalanceQueryRouteUsesInjectedProductionService(t *testing.T) {
	server, token, gateway := newCommandCenterAPITestServer(t)
	body := bytes.NewBufferString(`{"device_id":"wwan0"}`)
	response := performAPIRequest(server.newRouter(), http.MethodPost, "/api/balance/queries", token, body)
	if response.Code != http.StatusAccepted || gateway.smsCalls != 1 {
		t.Fatalf("balance status=%d calls=%d body=%s", response.Code, gateway.smsCalls, response.Body.String())
	}
}

func TestCommandEventStreamResumesAfterLastEventID(t *testing.T) {
	server, token, _ := newCommandCenterAPITestServer(t)
	if _, err := server.commandCenter.Execute(context.Background(), commandcenter.ExecuteRequest{Input: "/list"}); err != nil {
		t.Fatal(err)
	}
	events := waitForAPIEvents(t, server.commandCenter, 2)
	httpServer := httptest.NewServer(server.newRouter())
	defer httpServer.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	request, _ := http.NewRequestWithContext(ctx, http.MethodGet, httpServer.URL+"/api/commands/events/stream", nil)
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("Last-Event-ID", fmt.Sprint(events[0].ID))
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	scanner := bufio.NewScanner(response.Body)
	for scanner.Scan() {
		if strings.HasPrefix(scanner.Text(), "id:") {
			if got := strings.TrimSpace(strings.TrimPrefix(scanner.Text(), "id:")); got != fmt.Sprint(events[1].ID) {
				t.Fatalf("resumed event id = %s, want %d", got, events[1].ID)
			}
			return
		}
	}
	t.Fatalf("stream ended without event: %v", scanner.Err())
}

func newCommandCenterAPITestServer(t *testing.T) (*Server, string, *apiBalanceGateway) {
	t.Helper()
	database, err := gorm.Open(sqlite.Open(fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name())), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := database.AutoMigrate(&db.CommandExecution{}, &db.CommandEvent{}, &db.BalanceQuery{}); err != nil {
		t.Fatal(err)
	}
	commands := notify.NewCommandService(map[string]notify.CommandHandler{
		"list": func(_ notify.CommandContext, _ []string) string { return "设备列表 / 完成" },
	})
	gateway := &apiBalanceGateway{}
	server := &Server{auth: config.WebConfig{Username: "admin", Password: "secret"},
		commandCenter: commandcenter.NewService(commands, commandcenter.NewDatabaseStore(database)),
		balance:       balance.NewService(gateway, balance.NewDatabaseStore(database), apiRuleResolver{}),
		shutdownCh:    make(chan struct{}),
	}
	token, _, err := server.issueSessionToken()
	if err != nil {
		t.Fatal(err)
	}
	return server, token, gateway
}

func performAPIRequest(handler http.Handler, method, path, token string, body *bytes.Buffer) *httptest.ResponseRecorder {
	var request *http.Request
	if body == nil {
		request = httptest.NewRequest(method, path, nil)
	} else {
		request = httptest.NewRequest(method, path, body)
		request.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	return recorder
}

func waitForAPIEvents(t *testing.T, service *commandcenter.Service, count int) []commandcenter.Event {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		events, err := service.ListEvents(context.Background(), 0, 20)
		if err != nil {
			t.Fatal(err)
		}
		if len(events) >= count {
			return events
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("timed out waiting for command events")
	return nil
}
