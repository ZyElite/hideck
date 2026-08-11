package openwrt

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"
)

type environmentStub struct{ err error }

func (s environmentStub) Validate() error { return s.err }

type commandCall struct {
	name string
	args []string
}

type executorStub struct {
	calls []commandCall
	err   error
}

func (s *executorStub) Run(_ context.Context, name string, args ...string) ([]byte, error) {
	s.calls = append(s.calls, commandCall{name: name, args: append([]string(nil), args...)})
	return []byte("ubus detail"), s.err
}

func TestDisabledMapperMakesNoExecutorCalls(t *testing.T) {
	executor := &executorStub{}
	mapper := NewMapperWithDependencies(false, environmentStub{}, executor)
	if err := mapper.Add(context.Background(), "wwan0", "qmimux0"); err != nil {
		t.Fatalf("Add() error = %v", err)
	}
	if err := mapper.Remove(context.Background(), "wwan0"); err != nil {
		t.Fatalf("Remove() error = %v", err)
	}
	if len(executor.calls) != 0 {
		t.Fatalf("executor calls = %v, want none", executor.calls)
	}
}

func TestMapperUsesStructuredUbusArguments(t *testing.T) {
	executor := &executorStub{}
	mapper := NewMapperWithDependencies(true, environmentStub{}, executor)
	if err := mapper.Add(context.Background(), "wwan-0", "qmimux0"); err != nil {
		t.Fatalf("Add() error = %v", err)
	}
	if len(executor.calls) != 1 {
		t.Fatalf("calls = %d, want 1", len(executor.calls))
	}
	call := executor.calls[0]
	if call.name != "ubus" || len(call.args) != 4 {
		t.Fatalf("call = %+v", call)
	}
	if !reflect.DeepEqual(call.args[:3], []string{"call", "network", "add_dynamic"}) {
		t.Fatalf("args = %v", call.args)
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(call.args[3]), &payload); err != nil {
		t.Fatalf("payload is not JSON: %v", err)
	}
	if payload["name"] != "vohive_wwan-0" || payload["device"] != "qmimux0" || payload["proto"] != "none" {
		t.Fatalf("payload = %#v", payload)
	}
}

func TestMapperRejectsCommandInjectionInputs(t *testing.T) {
	executor := &executorStub{}
	mapper := NewMapperWithDependencies(true, environmentStub{}, executor)
	for _, test := range []struct {
		deviceID string
		iface    string
	}{
		{deviceID: "wwan0;reboot", iface: "wwan0"},
		{deviceID: "wwan0", iface: "wwan0$(reboot)"},
	} {
		if err := mapper.Add(context.Background(), test.deviceID, test.iface); err == nil {
			t.Fatalf("Add(%q, %q) expected validation error", test.deviceID, test.iface)
		}
	}
	if len(executor.calls) != 0 {
		t.Fatalf("executor calls = %v, want none", executor.calls)
	}
}

func TestMapperRejectsUnsupportedEnvironment(t *testing.T) {
	executor := &executorStub{}
	mapper := NewMapperWithDependencies(true, environmentStub{err: errors.New("not OpenWrt")}, executor)
	err := mapper.Add(context.Background(), "wwan0", "wwan0")
	if err == nil || err.Error() != "not OpenWrt" {
		t.Fatalf("Add() error = %v", err)
	}
	if len(executor.calls) != 0 {
		t.Fatalf("executor calls = %v, want none", executor.calls)
	}
}

func TestMapperRebindsChangedQMAPInterfaceAndCleansUpIdempotently(t *testing.T) {
	executor := &executorStub{}
	mapper := NewMapperWithDependencies(true, environmentStub{}, executor)
	for _, iface := range []string{"qmimux0", "qmimux0", "qmimux1"} {
		if err := mapper.Add(context.Background(), "wwan0", iface); err != nil {
			t.Fatalf("Add(%q) error = %v", iface, err)
		}
	}
	if err := mapper.Remove(context.Background(), "wwan0"); err != nil {
		t.Fatalf("Remove() error = %v", err)
	}
	if err := mapper.Remove(context.Background(), "wwan0"); err != nil {
		t.Fatalf("second Remove() error = %v", err)
	}
	if len(executor.calls) != 4 {
		t.Fatalf("calls = %d, want add/remove/add/remove", len(executor.calls))
	}
	for _, index := range []int{1, 3} {
		want := []string{"call", "network.interface.vohive_wwan0", "remove", "{}"}
		if !reflect.DeepEqual(executor.calls[index].args, want) {
			t.Fatalf("remove call %d = %v", index, executor.calls[index].args)
		}
	}
}

func TestMapperSurfacesUbusFailureOutput(t *testing.T) {
	executor := &executorStub{err: errors.New("exit status 1")}
	mapper := NewMapperWithDependencies(true, environmentStub{}, executor)
	err := mapper.Add(context.Background(), "wwan0", "wwan0")
	if err == nil || !errors.Is(err, executor.err) {
		t.Fatalf("Add() error = %v", err)
	}
}
