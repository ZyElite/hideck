package device

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/iniwex5/vohive/internal/backend"
	"github.com/iniwex5/vohive/internal/config"
	mbimcore "github.com/iniwex5/vohive/internal/mbim"
)

type fakeController struct {
	connected      bool
	connErr        error
	rotated        int
	dataInterface  string
	connectHook    func()
	disconnectHook func()
}

func (f *fakeController) Connect() error {
	if f.connectHook != nil {
		f.connectHook()
	}
	f.connected = f.connErr == nil
	return f.connErr
}

func (f *fakeController) Disconnect() error {
	if f.disconnectHook != nil {
		f.disconnectHook()
	}
	f.connected = false
	return nil
}
func (f *fakeController) IsConnected() bool { return f.connected }
func (f *fakeController) DataInterface() string {
	if f.dataInterface != "" {
		return f.dataInterface
	}
	return "wwan0"
}
func (f *fakeController) RotateIP() error {
	f.rotated++
	return nil
}
func (f *fakeController) GetPrivateIP() string                        { return "10.1.2.3" }
func (f *fakeController) GetPrivateIPv6() string                      { return "" }
func (f *fakeController) GetPublicIPv4AndV6NoCache() (string, string) { return "203.0.113.1", "" }

func TestNetControllerPrefersQMIThenMBIM(t *testing.T) {
	w := &Worker{}
	if w.NetworkController() != nil {
		t.Fatal("empty worker should have nil controller")
	}
}

func TestWorkerStartNetworkUsesController(t *testing.T) {
	fc := &fakeController{}
	w := &Worker{
		netOverride: fc,
		Config:      config.DeviceConfig{NetworkEnabled: true},
	}
	if err := w.StartNetwork(); err != nil {
		t.Fatalf("StartNetwork: %v", err)
	}
	if !fc.connected {
		t.Fatal("controller.Connect was not called")
	}
	if err := w.StopNetwork(); err != nil {
		t.Fatalf("StopNetwork: %v", err)
	}
	if fc.connected {
		t.Fatal("controller.Disconnect was not called")
	}
}

type dynamicInterfaceMapperStub struct {
	enabled     bool
	validateErr error
	addErr      error
	removeErr   error
	events      *[]string
	adds        []string
	removes     []string
}

func (s *dynamicInterfaceMapperStub) Enabled() bool     { return s.enabled }
func (s *dynamicInterfaceMapperStub) SetEnabled(v bool) { s.enabled = v }
func (s *dynamicInterfaceMapperStub) Validate() error   { return s.validateErr }
func (s *dynamicInterfaceMapperStub) Add(_ context.Context, deviceID, iface string) error {
	s.adds = append(s.adds, deviceID+":"+iface)
	if s.events != nil {
		*s.events = append(*s.events, "map-add")
	}
	return s.addErr
}
func (s *dynamicInterfaceMapperStub) Remove(_ context.Context, deviceID string) error {
	s.removes = append(s.removes, deviceID)
	if s.events != nil {
		*s.events = append(*s.events, "map-remove")
	}
	return s.removeErr
}

func TestWorkerNetworkLifecycleMapsActualDataInterface(t *testing.T) {
	events := []string{}
	mapper := &dynamicInterfaceMapperStub{enabled: true, events: &events}
	pool := NewPoolWithDynamicInterfaceMapper(&config.Config{}, mapper)
	controller := &fakeController{
		dataInterface:  "qmimux7",
		connectHook:    func() { events = append(events, "connect") },
		disconnectHook: func() { events = append(events, "disconnect") },
	}
	worker := &Worker{ID: "wwan0", Pool: pool, netOverride: controller}

	if err := worker.StartNetwork(); err != nil {
		t.Fatalf("StartNetwork() error = %v", err)
	}
	if strings.Join(events, ",") != "connect,map-add" {
		t.Fatalf("start events = %v", events)
	}
	if len(mapper.adds) != 1 || mapper.adds[0] != "wwan0:qmimux7" {
		t.Fatalf("mapper adds = %v", mapper.adds)
	}
	events = events[:0]
	if err := worker.StopNetwork(); err != nil {
		t.Fatalf("StopNetwork() error = %v", err)
	}
	if strings.Join(events, ",") != "map-remove,disconnect" {
		t.Fatalf("stop events = %v", events)
	}
}

func TestWorkerStartNetworkRollsBackConnectionWhenMappingFails(t *testing.T) {
	mapper := &dynamicInterfaceMapperStub{enabled: true, addErr: errors.New("ubus failed")}
	pool := NewPoolWithDynamicInterfaceMapper(&config.Config{}, mapper)
	controller := &fakeController{dataInterface: "wwan0"}
	worker := &Worker{ID: "wwan0", Pool: pool, netOverride: controller}

	err := worker.StartNetwork()
	if err == nil || !strings.Contains(err.Error(), "ubus failed") {
		t.Fatalf("StartNetwork() error = %v", err)
	}
	if controller.connected {
		t.Fatal("controller remained connected after mapping failure")
	}
}

func TestConfigureOpenWRTDynamicInterfacesRejectsUnsupportedSystem(t *testing.T) {
	mapper := &dynamicInterfaceMapperStub{validateErr: errors.New("not OpenWrt")}
	pool := NewPoolWithDynamicInterfaceMapper(&config.Config{}, mapper)
	err := pool.ConfigureOpenWRTDynamicInterfaces(context.Background(), true)
	if err == nil || err.Error() != "not OpenWrt" {
		t.Fatalf("ConfigureOpenWRTDynamicInterfaces() error = %v", err)
	}
	if mapper.enabled || len(mapper.adds) != 0 {
		t.Fatalf("mapper state enabled=%v adds=%v", mapper.enabled, mapper.adds)
	}
}

func TestConfigureOpenWRTDynamicInterfacesReconcilesConnectedWorkers(t *testing.T) {
	mapper := &dynamicInterfaceMapperStub{}
	pool := NewPoolWithDynamicInterfaceMapper(&config.Config{}, mapper)
	controller := &fakeController{connected: true, dataInterface: "wwan9"}
	worker := &Worker{ID: "dev9", Pool: pool, netOverride: controller}
	pool.workers[worker.ID] = worker

	if err := pool.ConfigureOpenWRTDynamicInterfaces(context.Background(), true); err != nil {
		t.Fatalf("enable error = %v", err)
	}
	if !mapper.enabled || len(mapper.adds) != 1 || mapper.adds[0] != "dev9:wwan9" {
		t.Fatalf("enable state=%v adds=%v", mapper.enabled, mapper.adds)
	}
	if err := pool.ConfigureOpenWRTDynamicInterfaces(context.Background(), false); err != nil {
		t.Fatalf("disable error = %v", err)
	}
	if mapper.enabled || len(mapper.removes) != 1 || mapper.removes[0] != "dev9" {
		t.Fatalf("disable state=%v removes=%v", mapper.enabled, mapper.removes)
	}
}

type startNetworkMBIMBackendStub struct {
	events  *[]string
	serving *backend.ServingSystem
}

func (s *startNetworkMBIMBackendStub) GetIMEI(context.Context) (string, error)     { return "", nil }
func (s *startNetworkMBIMBackendStub) GetIMSI(context.Context) (string, error)     { return "", nil }
func (s *startNetworkMBIMBackendStub) GetICCID(context.Context) (string, error)    { return "", nil }
func (s *startNetworkMBIMBackendStub) GetMSISDN(context.Context) (string, error)   { return "", nil }
func (s *startNetworkMBIMBackendStub) GetRevision(context.Context) (string, error) { return "", nil }
func (s *startNetworkMBIMBackendStub) GetSignalInfo(context.Context) (*backend.SignalInfo, error) {
	return &backend.SignalInfo{}, nil
}
func (s *startNetworkMBIMBackendStub) GetServingSystem(context.Context) (*backend.ServingSystem, error) {
	if s.serving == nil {
		return &backend.ServingSystem{RegStatus: 1, PSAttached: true}, nil
	}
	return s.serving, nil
}
func (s *startNetworkMBIMBackendStub) IsSimInserted(context.Context) (bool, error) { return true, nil }
func (s *startNetworkMBIMBackendStub) GetNativeMCCMNC(context.Context) (string, string, error) {
	return "", "", nil
}
func (s *startNetworkMBIMBackendStub) GetNativeSPN(context.Context) (string, error) { return "", nil }
func (s *startNetworkMBIMBackendStub) GetSIMMetadata(context.Context) (*backend.SIMMetadata, error) {
	return nil, nil
}
func (s *startNetworkMBIMBackendStub) SendSMS(context.Context, string, string) error { return nil }
func (s *startNetworkMBIMBackendStub) ReadSMS(context.Context, int) (*backend.SMS, error) {
	return nil, nil
}
func (s *startNetworkMBIMBackendStub) DeleteSMS(context.Context, int) error { return nil }
func (s *startNetworkMBIMBackendStub) ListSMS(context.Context) ([]backend.SMSSummary, error) {
	return nil, nil
}
func (s *startNetworkMBIMBackendStub) DeleteAllSMS(context.Context) error { return nil }
func (s *startNetworkMBIMBackendStub) SetOperatingMode(context.Context, backend.OperatingMode) error {
	return nil
}
func (s *startNetworkMBIMBackendStub) GetOperatingMode(context.Context) (backend.OperatingMode, error) {
	return backend.ModeOnline, nil
}
func (s *startNetworkMBIMBackendStub) Reboot(context.Context) error { return nil }
func (s *startNetworkMBIMBackendStub) OpenLogicalChannel(context.Context, string) (int, error) {
	return 0, nil
}
func (s *startNetworkMBIMBackendStub) CloseLogicalChannel(context.Context, int) error {
	return nil
}
func (s *startNetworkMBIMBackendStub) TransmitAPDU(context.Context, int, string) (string, error) {
	return "", nil
}
func (s *startNetworkMBIMBackendStub) TransmitBasicAPDU(context.Context, string) (string, error) {
	return "", nil
}
func (s *startNetworkMBIMBackendStub) Mode() string { return backend.BackendMBIM }
func (s *startNetworkMBIMBackendStub) Close() error { return nil }
func (s *startNetworkMBIMBackendStub) SetOperatorSelection(_ context.Context, req backend.SetOperatorSelectionRequest) (backend.OperatorSelection, error) {
	return backend.OperatorSelection{Mode: req.Mode, PLMN: req.PLMN}, nil
}
func (s *startNetworkMBIMBackendStub) AttachPacketService(context.Context) error {
	*s.events = append(*s.events, "attach")
	if s.serving != nil {
		s.serving.PSAttached = true
	}
	return nil
}

func TestWorkerStartNetworkEnsuresMBIMRegistrationBeforeConnect(t *testing.T) {
	events := []string{}
	ctrl := &startNetworkMBIMBackendStub{
		events:  &events,
		serving: &backend.ServingSystem{RegStatus: 1, PSAttached: false},
	}
	w := &Worker{
		ID:       "dev-mbim",
		Config:   config.DeviceConfig{ID: "dev-mbim", DeviceBackend: backend.BackendMBIM, NetworkEnabled: true},
		Backend:  ctrl,
		MBIMCore: &mbimcore.Manager{},
		netOverride: &fakeController{connectHook: func() {
			events = append(events, "connect")
		}},
	}

	if err := w.StartNetwork(); err != nil {
		t.Fatalf("StartNetwork: %v", err)
	}
	if strings.Join(events, ",") != "attach,connect" {
		t.Fatalf("events=%v want attach before connect", events)
	}
}
