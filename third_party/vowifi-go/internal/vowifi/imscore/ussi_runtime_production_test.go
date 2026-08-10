package imscore

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

const (
	testUSSIMenuXML  = `<?xml version="1.0"?><ussd-data><language>en</language><ussd-string>1. Balance&#10;2. Data</ussd-string><UnstructuredSS-Request/></ussd-data>`
	testUSSIFinalXML = `<?xml version="1.0"?><ussd-data><language>en</language><ussd-string>Balance: 10</ussd-string><UnstructuredSS-Notify/></ussd-data>`
)

func TestUSSIProductionUDPLifecycle(t *testing.T) {
	registrar, service := newRegisteredUDPUSSIService(t)
	serverResult := make(chan error, 1)
	go func() { serverResult <- serveUDPUSSILifecycle(registrar) }()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	initial, err := service.SendUSSD(ctx, "*100#")
	if err != nil {
		t.Fatal(err)
	}
	if initial.Status != 1 || initial.Text != "1. Balance\n2. Data" {
		t.Fatalf("initial result = %+v", initial)
	}
	final, err := service.ContinueUSSD(ctx, initial.SessionID, "1")
	if err != nil {
		t.Fatal(err)
	}
	if final.Status != 0 || final.Text != "Balance: 10" {
		t.Fatalf("continued result = %+v", final)
	}
	second, err := service.SendUSSD(ctx, "*101#")
	if err != nil {
		t.Fatal(err)
	}
	if err := service.CancelUSSD(ctx, second.SessionID); err != nil {
		t.Fatal(err)
	}
	if active := service.GetActiveUSSDSession(); active != "" {
		t.Fatalf("active session after cancel = %q", active)
	}
	if err := <-serverResult; err != nil {
		t.Fatal(err)
	}
}

func TestUSSIProductionUDPTimeoutClosesAndClearsSession(t *testing.T) {
	registrar, service := newRegisteredUDPUSSIService(t)
	serverResult := make(chan error, 1)
	go func() { serverResult <- serveUDPUSSITimeout(registrar) }()

	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Millisecond)
	defer cancel()
	_, err := service.SendUSSD(ctx, "*100#")
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("SendUSSD error = %v", err)
	}
	if active := service.GetActiveUSSDSession(); active != "" {
		t.Fatalf("active session after timeout = %q", active)
	}
	if err := <-serverResult; err != nil {
		t.Fatal(err)
	}
}

func TestUSSIProductionStopWakesBlockedSend(t *testing.T) {
	registrar, service := newRegisteredUDPUSSIService(t)
	serverReady := make(chan error, 1)
	go func() { serverReady <- serveUDPUSSIUntilACK(registrar) }()
	sendResult := make(chan error, 1)
	go func() {
		_, err := service.SendUSSD(context.Background(), "*100#")
		sendResult <- err
	}()
	if err := <-serverReady; err != nil {
		t.Fatal(err)
	}
	service.StopCurrent()
	select {
	case err := <-sendResult:
		if err == nil || !strings.Contains(err.Error(), "service stopped") {
			t.Fatalf("SendUSSD error = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Service.Stop did not release blocked USSI send")
	}
	if active := service.GetActiveUSSDSession(); active != "" {
		t.Fatalf("active session after Stop = %q", active)
	}
}
