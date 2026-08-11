package manager

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/iniwex5/quectel-qmi-go/pkg/qmi"
)

func TestEnsureModemDataFormatEnablesQMAP(t *testing.T) {
	m := newRecoveryTestManager()
	m.cfg.MuxID = 1
	m.cfg.Timeouts.StatusCheck = defaultTimeouts.StatusCheck
	current := desiredDataFormat(false)
	setCalls := 0
	m.getDataFormatHook = func(context.Context) (*qmi.DataFormat, error) {
		copy := current
		return &copy, nil
	}
	m.setDataFormatHook = func(_ context.Context, format qmi.DataFormat) error {
		setCalls++
		current = format
		return nil
	}

	if err := m.ensureModemDataFormat(context.Background()); err != nil {
		t.Fatalf("ensureModemDataFormat() error = %v", err)
	}
	if setCalls != 1 {
		t.Fatalf("set calls = %d, want 1", setCalls)
	}
	if current.UlDataAggregation != qmi.DataAggregationProtocolQMAP ||
		current.DlDataAggregation != qmi.DataAggregationProtocolQMAP {
		t.Fatalf("QMAP aggregation not selected: %+v", current)
	}
}

func TestEnsureModemDataFormatRestoresPlainRawIP(t *testing.T) {
	m := newRecoveryTestManager()
	m.cfg.MuxID = 0
	m.cfg.Timeouts.StatusCheck = defaultTimeouts.StatusCheck
	current := desiredDataFormat(true)
	m.getDataFormatHook = func(context.Context) (*qmi.DataFormat, error) {
		copy := current
		return &copy, nil
	}
	m.setDataFormatHook = func(_ context.Context, format qmi.DataFormat) error {
		current = format
		return nil
	}

	if err := m.ensureModemDataFormat(context.Background()); err != nil {
		t.Fatalf("ensureModemDataFormat() error = %v", err)
	}
	if !dataFormatMatches(&current, desiredDataFormat(false)) {
		t.Fatalf("plain Raw-IP format not restored: %+v", current)
	}
}

func TestEnsureModemDataFormatSkipsMatchingFormat(t *testing.T) {
	m := newRecoveryTestManager()
	m.cfg.MuxID = 1
	current := desiredDataFormat(true)
	m.getDataFormatHook = func(context.Context) (*qmi.DataFormat, error) { return &current, nil }
	m.setDataFormatHook = func(context.Context, qmi.DataFormat) error {
		t.Fatal("matching format must not be written again")
		return nil
	}

	if err := m.ensureModemDataFormat(context.Background()); err != nil {
		t.Fatalf("ensureModemDataFormat() error = %v", err)
	}
}

func TestEnsureModemDataFormatExposesQueryFailure(t *testing.T) {
	wantErr := errors.New("WDA query failed")
	m := newRecoveryTestManager()
	m.getDataFormatHook = func(context.Context) (*qmi.DataFormat, error) { return nil, wantErr }
	m.setDataFormatHook = func(context.Context, qmi.DataFormat) error {
		t.Fatal("format must not be guessed after query failure")
		return nil
	}

	err := m.ensureModemDataFormat(context.Background())
	if !errors.Is(err, wantErr) {
		t.Fatalf("ensureModemDataFormat() error = %v, want wrapped %v", err, wantErr)
	}
}

func TestEnsureModemDataFormatRejectsVerificationMismatch(t *testing.T) {
	m := newRecoveryTestManager()
	m.cfg.MuxID = 1
	current := desiredDataFormat(false)
	m.getDataFormatHook = func(context.Context) (*qmi.DataFormat, error) { return &current, nil }
	m.setDataFormatHook = func(context.Context, qmi.DataFormat) error { return nil }

	err := m.ensureModemDataFormat(context.Background())
	if err == nil || !strings.Contains(err.Error(), "verification mismatch") {
		t.Fatalf("ensureModemDataFormat() error = %v, want verification mismatch", err)
	}
}
