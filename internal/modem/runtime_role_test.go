package modem

import (
	"reflect"
	"testing"
	"time"

	"github.com/yibaiba/hideck/internal/config"
	"github.com/yibaiba/hideck/pkg/smscodec"
)

func TestNewSMSAuxiliaryRequiresATPort(t *testing.T) {
	_, err := NewSMSAuxiliary(config.DeviceConfig{ID: "dev-mbim", DeviceBackend: "mbim"})
	if err == nil || err.Error() != "AT port not configured" {
		t.Fatalf("NewSMSAuxiliary() error = %v", err)
	}
}

func TestSMSAuxiliaryRunsATWithoutFullModemInitialization(t *testing.T) {
	m, err := NewSMSAuxiliary(config.DeviceConfig{
		ID:            "dev-mbim",
		DeviceBackend: "mbim",
		ATPort:        "/dev/ttyUSB2",
	})
	if err != nil {
		t.Fatalf("NewSMSAuxiliary() error = %v", err)
	}
	if !m.IsSMSAuxiliary() {
		t.Fatal("IsSMSAuxiliary() = false")
	}
	wantCommands := []string{"ATE0", "AT+CMGF=0", "AT+CNMI=2,1,0,0,0"}
	if got := m.initializationCommands(); !reflect.DeepEqual(got, wantCommands) {
		t.Fatalf("initializationCommands() = %v, want %v", got, wantCommands)
	}
	m.running = true
	if !m.CanExecuteAT() {
		t.Fatal("CanExecuteAT() = false for running MBIM SMS auxiliary")
	}
}

func TestSMSAuxiliaryQueuesManualATAndSMSOnOneScheduler(t *testing.T) {
	m, err := NewSMSAuxiliary(config.DeviceConfig{
		ID:            "dev-mbim",
		DeviceBackend: "mbim",
		ATPort:        "/dev/ttyUSB2",
	})
	if err != nil {
		t.Fatalf("NewSMSAuxiliary() error = %v", err)
	}
	m.running = true
	m.healthy = true

	manualDone := make(chan error, 1)
	smsDone := make(chan error, 1)
	go func() {
		_, err := m.ExecuteAT("AT+CSQ", time.Second)
		manualDone <- err
	}()
	go func() {
		smsDone <- m.SendSMSWithOptions("10086", "hello", smscodec.SubmitOptions{})
	}()

	commands := make([]string, 0, 3)
	for len(commands) < 3 {
		select {
		case req := <-m.cmdChanHigh:
			commands = append(commands, req.cmd)
			if req.interactive {
				req.respChan <- "+CMGS: 1"
			} else {
				req.respChan <- "OK"
			}
		case req := <-m.cmdChan:
			commands = append(commands, req.cmd)
			req.respChan <- "OK"
		case <-time.After(time.Second):
			t.Fatalf("timed out waiting for shared scheduler, commands=%v", commands)
		}
	}
	if err := <-manualDone; err != nil {
		t.Fatalf("manual AT error = %v", err)
	}
	if err := <-smsDone; err != nil {
		t.Fatalf("SMS error = %v", err)
	}
	want := map[string]bool{"AT+CSQ": false, "AT+CMGF=0": false}
	for _, command := range commands {
		if _, ok := want[command]; ok {
			want[command] = true
		}
	}
	for command, seen := range want {
		if !seen {
			t.Fatalf("shared scheduler did not receive %s: %v", command, commands)
		}
	}
}
