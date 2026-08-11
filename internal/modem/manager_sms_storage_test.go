package modem

import (
	"errors"
	"reflect"
	"testing"
	"time"
)

func TestHandleCMTIUsesIndicatedStorageForReadAndDelete(t *testing.T) {
	m := newRunningTestManager(t)
	validPDU := "079144872000302320048102020000625061028204401AD9775D0E72D7DBE2B21C949E8360B75A4E7683D16AB71B"

	done := make(chan []string, 1)
	go func() {
		done <- respondToCommands(t, m, 5, func(req commandRequest) {
			switch req.cmd {
			case "AT+CPMS?":
				req.respChan <- "\r\n+CPMS: \"SM\",0,10,\"SM\",0,10,\"SM\",0,10\r\n\r\nOK\r\n"
			case `AT+CPMS="ME","ME","ME"`:
				req.respChan <- "OK"
			case "AT+CMGR=7":
				req.respChan <- "\r\n+CMGR: 0,,38\r\n" + validPDU + "\r\n\r\nOK\r\n"
			case "AT+CMGD=7":
				req.respChan <- "OK"
			case `AT+CPMS="SM","SM","SM"`:
				req.respChan <- "OK"
			default:
				req.errChan <- nil
			}
		})
	}()

	m.handleURC(`+CMTI: "ME",7`)

	var got []string
	select {
	case got = <-done:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for storage-aware CMTI handling")
	}
	want := []string{
		"AT+CPMS?",
		`AT+CPMS="ME","ME","ME"`,
		"AT+CMGR=7",
		"AT+CMGD=7",
		`AT+CPMS="SM","SM","SM"`,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("commands=%#v want %#v", got, want)
	}
}

func TestReadSMSRetainsMessageWhenIdentityNotReady(t *testing.T) {
	m := newRunningTestManager(t)
	checks := 0
	m.SetSMSReadinessCheck(func() error {
		checks++
		return errors.New("identity unknown")
	})

	m.ReadAndProcessSMS("7")

	if checks != 1 {
		t.Fatalf("readiness checks=%d, want 1", checks)
	}
	if len(m.cmdChan) != 0 {
		t.Fatalf("queued AT commands=%d, want no read or delete", len(m.cmdChan))
	}
}

func TestReadSMSRetainsMessageWhenProcessorFails(t *testing.T) {
	m := newRunningTestManager(t)
	validPDU := "079144872000302320048102020000625061028204401AD9775D0E72D7DBE2B21C949E8360B75A4E7683D16AB71B"
	m.SetSMSProcessor(func(sender, content string, timestamp time.Time) error {
		return errors.New("database unavailable")
	})

	done := make(chan []string, 1)
	go func() {
		done <- respondToCommands(t, m, 1, func(req commandRequest) {
			req.respChan <- "\r\n+CMGR: 0,,38\r\n" + validPDU + "\r\n\r\nOK\r\n"
		})
	}()
	m.ReadAndProcessSMS("7")
	commands := <-done

	if !reflect.DeepEqual(commands, []string{"AT+CMGR=7"}) {
		t.Fatalf("commands=%v, want read without delete", commands)
	}
}
