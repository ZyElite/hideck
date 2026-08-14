package api

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/yibaiba/hideck/internal/config"
	"github.com/yibaiba/hideck/internal/device"
)

type backendSwitchPoolStub struct {
	worker      *device.Worker
	removeCalls int
	addCalls    int
	added       config.DeviceConfig
	removeErr   error
	addErr      error
}

func (p *backendSwitchPoolStub) GetWorker(string) *device.Worker { return p.worker }

func (p *backendSwitchPoolStub) RemoveWorker(string) error {
	p.removeCalls++
	if p.removeErr == nil {
		p.worker = nil
	}
	return p.removeErr
}

func (p *backendSwitchPoolStub) AddWorkerFromConfig(cfg config.DeviceConfig) (*device.Worker, error) {
	p.addCalls++
	p.added = cfg
	if p.addErr != nil {
		return nil, p.addErr
	}
	return &device.Worker{ID: cfg.ID, Config: cfg}, nil
}

type backendAttachmentWaiterStub struct {
	attachments       []device.BackendAttachment
	errors            []error
	targets           []string
	atPortHints       []string
	allowATRecoveries []bool
	calls             int
}

func (w *backendAttachmentWaiterStub) WaitWithHint(
	_ context.Context,
	query device.BackendAttachmentQuery,
) (device.BackendAttachment, error) {
	w.targets = append(w.targets, query.TargetBackend)
	w.atPortHints = append(w.atPortHints, query.ATPortHint)
	w.allowATRecoveries = append(w.allowATRecoveries, query.AllowATIdentityRecovery)
	index := w.calls
	w.calls++
	if index < len(w.errors) && w.errors[index] != nil {
		return device.BackendAttachment{}, w.errors[index]
	}
	for _, attachment := range w.attachments {
		if query.TargetBackend == "" || attachment.Backend == query.TargetBackend {
			return attachment, nil
		}
	}
	return device.BackendAttachment{}, fmt.Errorf("unexpected discovery target %q on call %d", query.TargetBackend, index)
}

type backendSwitchATStub struct {
	queryMode int
	commands  []string
	closeErr  error
}

func (s *backendSwitchATStub) Execute(command string, _ time.Duration) (string, error) {
	s.commands = append(s.commands, command)
	if command == "AT+QCFG=\"usbnet\"?" {
		return fmt.Sprintf("\r\n+QCFG: \"usbnet\",%d\r\nOK\r\n", s.queryMode), nil
	}
	return "\r\nOK\r\n", nil
}

func (s *backendSwitchATStub) Close() error { return s.closeErr }

func TestBackendSwitchServiceBidirectional(t *testing.T) {
	tests := []struct {
		name       string
		current    string
		target     string
		queryMode  int
		targetMode int
	}{
		{name: "qmi to mbim", current: "qmi", target: "mbim", queryMode: 0, targetMode: 2},
		{name: "mbim to qmi", current: "mbim", target: "qmi", queryMode: 2, targetMode: 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			service, pool, waiter, at, persistCalls := newBackendSwitchTestService(tt.current, tt.target, tt.queryMode)
			result, err := service.Switch(context.Background(), backendSwitchTestRequest(tt.current, tt.target))
			if err != nil {
				t.Fatalf("Switch() error = %v", err)
			}
			if !result.HardwareChanged || !result.HardwareVerified || !result.Persisted || !result.WorkerStarted {
				t.Fatalf("Switch() result = %+v", result)
			}
			if result.CurrentAttachment == nil || result.CurrentAttachment.ATPort != "/dev/ttyUSB6" {
				t.Fatalf("current attachment = %+v", result.CurrentAttachment)
			}
			if *persistCalls != 1 || pool.removeCalls != 1 || pool.addCalls != 1 {
				t.Fatalf("calls persist=%d remove=%d add=%d", *persistCalls, pool.removeCalls, pool.addCalls)
			}
			if pool.added.DeviceBackend != tt.target || pool.added.USBNetMode == nil || *pool.added.USBNetMode != tt.targetMode {
				t.Fatalf("added config = %+v", pool.added)
			}
			wantSet := fmt.Sprintf("AT+QCFG=\"usbnet\",%d", tt.targetMode)
			if len(at.commands) != 3 || at.commands[1] != wantSet || at.commands[2] != "AT+CFUN=1,1" {
				t.Fatalf("AT commands = %v", at.commands)
			}
			if len(waiter.targets) != 1 || waiter.targets[0] != tt.target {
				t.Fatalf("discovery targets = %v", waiter.targets)
			}
			if len(waiter.atPortHints) != 1 || waiter.atPortHints[0] != "/dev/ttyUSB6" {
				t.Fatalf("discovery AT port hints = %v", waiter.atPortHints)
			}
		})
	}
}

func TestBackendSwitchServiceDiscoveryTimeoutPreservesConfig(t *testing.T) {
	service, pool, waiter, _, persistCalls := newBackendSwitchTestService("qmi", "mbim", 0)
	pool.worker = nil
	waiter.errors = []error{context.DeadlineExceeded}

	result, err := service.Switch(context.Background(), backendSwitchTestRequest("qmi", "mbim"))
	assertBackendSwitchFailure(t, err, "discover_current")
	if result.Persisted || *persistCalls != 0 || pool.removeCalls != 0 || pool.addCalls != 0 {
		t.Fatalf("unexpected side effects result=%+v persist=%d pool=%+v", result, *persistCalls, pool)
	}
}

func TestBackendSwitchServiceAmbiguousEnumerationStopsBeforeMutation(t *testing.T) {
	service, pool, waiter, _, persistCalls := newBackendSwitchTestService("qmi", "mbim", 0)
	pool.worker = nil
	waiter.errors = []error{errors.New("IMEI 对应到 2 个设备路径，拒绝自动选择")}

	_, err := service.Switch(context.Background(), backendSwitchTestRequest("qmi", "mbim"))
	assertBackendSwitchFailure(t, err, "discover_current")
	if *persistCalls != 0 || pool.removeCalls != 0 {
		t.Fatalf("persist=%d remove=%d", *persistCalls, pool.removeCalls)
	}
}

func TestBackendSwitchServicePersistFailureReturnsVerifiedHardwareState(t *testing.T) {
	service, pool, _, _, persistCalls := newBackendSwitchTestService("qmi", "mbim", 0)
	service.persist = func(string, string, config.DeviceConfig) error {
		(*persistCalls)++
		return errors.New("disk full")
	}

	result, err := service.Switch(context.Background(), backendSwitchTestRequest("qmi", "mbim"))
	assertBackendSwitchFailure(t, err, "persist_config")
	if !result.HardwareChanged || !result.HardwareVerified || result.Persisted || result.WorkerStarted {
		t.Fatalf("result = %+v", result)
	}
	if *persistCalls != 1 || pool.addCalls != 0 {
		t.Fatalf("persist=%d add=%d", *persistCalls, pool.addCalls)
	}
}

func TestBackendSwitchServiceStartFailureDoesNotHidePersistedState(t *testing.T) {
	service, pool, _, _, persistCalls := newBackendSwitchTestService("qmi", "mbim", 0)
	pool.addErr = errors.New("backend init failed")

	result, err := service.Switch(context.Background(), backendSwitchTestRequest("qmi", "mbim"))
	assertBackendSwitchFailure(t, err, "start_worker")
	if !result.Persisted || result.WorkerStarted || *persistCalls != 1 || pool.addCalls != 1 {
		t.Fatalf("result=%+v persist=%d add=%d", result, *persistCalls, pool.addCalls)
	}
}

func TestBackendSwitchServiceRepairsAfterRestartWithoutWorker(t *testing.T) {
	service, pool, waiter, _, persistCalls := newBackendSwitchTestService("qmi", "mbim", 2)
	pool.worker = nil

	result, err := service.Switch(context.Background(), backendSwitchTestRequest("qmi", "mbim"))
	if err != nil {
		t.Fatalf("Switch() error = %v", err)
	}
	if result.HardwareChanged || pool.removeCalls != 0 || pool.addCalls != 1 || *persistCalls != 1 {
		t.Fatalf("result=%+v remove=%d add=%d persist=%d", result, pool.removeCalls, pool.addCalls, *persistCalls)
	}
	if !reflect.DeepEqual(waiter.allowATRecoveries, []bool{true, false}) {
		t.Fatalf("AT identity recovery flags = %v", waiter.allowATRecoveries)
	}
}

func TestBackendSwitchServiceRejectsATAndUnknownHardwareMode(t *testing.T) {
	service, _, _, _, _ := newBackendSwitchTestService("at", "qmi", 0)
	_, err := service.Switch(context.Background(), backendSwitchTestRequest("at", "qmi"))
	assertBackendSwitchFailure(t, err, "validate")

	service, _, _, _, persistCalls := newBackendSwitchTestService("qmi", "mbim", 1)
	_, err = service.Switch(context.Background(), backendSwitchTestRequest("qmi", "mbim"))
	assertBackendSwitchFailure(t, err, "apply_hardware")
	if *persistCalls != 0 {
		t.Fatalf("persist calls = %d", *persistCalls)
	}
}

func newBackendSwitchTestService(
	current string,
	target string,
	queryMode int,
) (*backendSwitchService, *backendSwitchPoolStub, *backendAttachmentWaiterStub, *backendSwitchATStub, *int) {
	pool := &backendSwitchPoolStub{worker: &device.Worker{
		ID: "wwan1",
		Config: config.DeviceConfig{
			ATPort:        "/dev/ttyUSB6",
			ControlDevice: "/dev/cdc-wdm1",
			Interface:     "wwan1",
			USBPath:       "1-2",
		},
	}}
	waiter := &backendAttachmentWaiterStub{attachments: []device.BackendAttachment{
		backendSwitchTestAttachment(current),
		backendSwitchTestAttachment(target),
	}}
	at := &backendSwitchATStub{queryMode: queryMode}
	persistCalls := 0
	service := &backendSwitchService{
		pool:      pool,
		discovery: waiter,
		workerIMEI: func(context.Context, *device.Worker) (string, error) {
			return "866069053342612", nil
		},
		openAT: func(string) (manualATSession, error) {
			return at, nil
		},
		persist: func(string, string, config.DeviceConfig) error {
			persistCalls++
			return nil
		},
		configPath: "config.yaml",
	}
	return service, pool, waiter, at, &persistCalls
}

func backendSwitchTestRequest(current, target string) backendSwitchRequest {
	base := config.DeviceConfig{
		ID:            "wwan1",
		Name:          "test",
		ModemIMEI:     "866069053342612",
		DeviceBackend: current,
	}
	desired := base
	desired.DeviceBackend = target
	return backendSwitchRequest{Current: base, Desired: desired, Target: target}
}

func backendSwitchTestAttachment(backend string) device.BackendAttachment {
	return device.BackendAttachment{
		Backend:       backend,
		IMEI:          "866069053342612",
		ControlDevice: "/dev/cdc-wdm1",
		Interface:     "wwan1",
		USBPath:       "1-2",
		ATPort:        "/dev/ttyUSB6",
	}
}

func assertBackendSwitchFailure(t *testing.T, err error, stage string) {
	t.Helper()
	var failure *backendSwitchFailure
	if !errors.As(err, &failure) {
		t.Fatalf("error = %v, want backendSwitchFailure", err)
	}
	if failure.Stage != stage || !strings.Contains(err.Error(), stage) {
		t.Fatalf("failure = %+v, want stage %s", failure, stage)
	}
}
