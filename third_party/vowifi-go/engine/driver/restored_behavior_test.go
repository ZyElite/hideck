package driver

import (
	"errors"
	"reflect"
	"testing"

	"go.uber.org/multierr"
)

func TestNetToolErrorOriginalFormatting(t *testing.T) {
	cause := errors.New("boom")
	withArguments := &NetToolError{Op: "route add", Args: "10.0.0.0/24", Err: cause}
	if got := withArguments.Error(); got != "route add 10.0.0.0/24 失败: boom" {
		t.Fatalf("Error() = %q", got)
	}
	withoutArguments := &NetToolError{Op: "route add", Err: cause}
	if got := withoutArguments.Error(); got != "route add 失败: boom" {
		t.Fatalf("Error() without arguments = %q", got)
	}
	if !errors.Is(withArguments, cause) {
		t.Fatal("NetToolError did not unwrap its cause")
	}
}

func TestNetTxnRollbackRunsAllUndosInReverseAndJoinsErrors(t *testing.T) {
	firstErr := errors.New("first")
	thirdErr := errors.New("third")
	var order []int
	tx := &NetTxn{undos: []func() error{
		func() error { order = append(order, 1); return firstErr },
		func() error { order = append(order, 2); return nil },
		func() error { order = append(order, 3); return thirdErr },
	}}
	err := tx.Rollback()
	if !reflect.DeepEqual(order, []int{3, 2, 1}) {
		t.Fatalf("undo order = %v", order)
	}
	if got := multierr.Errors(err); !reflect.DeepEqual(got, []error{thirdErr, firstErr}) {
		t.Fatalf("rollback errors = %v", got)
	}
	if tx.undos != nil {
		t.Fatal("rollback retained undo functions")
	}
}

func TestNetTxnCommitDropsUndosWithoutRunningThem(t *testing.T) {
	called := false
	tx := &NetTxn{undos: []func() error{func() error { called = true; return nil }}}
	tx.Commit()
	if called || tx.undos != nil {
		t.Fatalf("commit called undo=%v or retained entries=%d", called, len(tx.undos))
	}
}

func TestXFRMCleanupCheckedRunsAllUndosInReverseAndJoinsErrors(t *testing.T) {
	firstErr := errors.New("first")
	thirdErr := errors.New("third")
	var order []int
	manager := &XFRMManager{undos: []func() error{
		func() error { order = append(order, 1); return firstErr },
		func() error { order = append(order, 2); return nil },
		func() error { order = append(order, 3); return thirdErr },
	}}
	err := manager.CleanupChecked()
	if !reflect.DeepEqual(order, []int{3, 2, 1}) {
		t.Fatalf("undo order = %v", order)
	}
	if got := multierr.Errors(err); !reflect.DeepEqual(got, []error{thirdErr, firstErr}) {
		t.Fatalf("cleanup errors = %v", got)
	}
	if manager.undos != nil {
		t.Fatal("cleanup retained undo functions")
	}
}

func TestEnsureIPv6EnabledChangesOnlyDisabledKeys(t *testing.T) {
	values := map[string]string{
		"net.ipv6.conf.all.disable_ipv6":     "1",
		"net.ipv6.conf.default.disable_ipv6": "0",
		"net.ipv6.conf.wwan0.disable_ipv6":   "1",
	}
	read := func(key string) (string, error) { return values[key], nil }
	write := func(key, value string) error { values[key] = value; return nil }
	changed, err := ensureIPv6Enabled(" wwan0 ", read, write)
	if err != nil {
		t.Fatalf("EnsureIPv6Enabled: %v", err)
	}
	want := []string{"net.ipv6.conf.all.disable_ipv6", "net.ipv6.conf.wwan0.disable_ipv6"}
	if !reflect.DeepEqual(changed, want) {
		t.Fatalf("changed keys = %v, want %v", changed, want)
	}
	for key, value := range values {
		if value != "0" {
			t.Errorf("%s remained %q", key, value)
		}
	}
}

func TestEnsureIPv6EnabledRejectsInvalidAndFailedReadback(t *testing.T) {
	readInvalid := func(string) (string, error) { return "2", nil }
	if _, err := ensureIPv6Enabled("", readInvalid, func(string, string) error { return nil }); err == nil {
		t.Fatal("invalid sysctl value was accepted")
	}
	readBackCount := 0
	readBack := func(string) (string, error) {
		readBackCount++
		return "1", nil
	}
	changed, err := ensureIPv6Enabled("", readBack, func(string, string) error { return nil })
	if err == nil {
		t.Fatal("failed sysctl readback was accepted")
	}
	if !reflect.DeepEqual(changed, []string{"net.ipv6.conf.all.disable_ipv6"}) {
		t.Fatalf("changed keys after failed readback = %v", changed)
	}
}

func TestSysctlPath(t *testing.T) {
	if got := sysctlPath(" net.ipv6.conf.all.disable_ipv6 "); got != "/proc/sys/net/ipv6/conf/all/disable_ipv6" {
		t.Fatalf("sysctlPath = %q", got)
	}
	for _, invalid := range []string{"", "../kernel.hostname", "net/ipv6/conf/all", "net..ipv6"} {
		if got := sysctlPath(invalid); got != "" {
			t.Errorf("sysctlPath(%q) = %q, want empty", invalid, got)
		}
	}
}
