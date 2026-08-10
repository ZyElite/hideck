package device

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/iniwex5/vohive/internal/backend"
	"github.com/iniwex5/vohive/internal/config"
)

type mbimRegistrationTestController struct {
	servingSeq     []*backend.ServingSystem
	modeSeq        []backend.OperatingMode
	attachCalls    int
	setModes       []backend.OperatingMode
	selections     []backend.SetOperatorSelectionRequest
	simInsertedSeq []bool
}

func (s *mbimRegistrationTestController) GetServingSystem(context.Context) (*backend.ServingSystem, error) {
	if len(s.servingSeq) == 0 {
		return &backend.ServingSystem{RegStatus: 1, PSAttached: true}, nil
	}
	out := s.servingSeq[0]
	if len(s.servingSeq) > 1 {
		s.servingSeq = s.servingSeq[1:]
	}
	return out, nil
}

func (s *mbimRegistrationTestController) GetOperatingMode(context.Context) (backend.OperatingMode, error) {
	if len(s.modeSeq) == 0 {
		return backend.ModeOnline, nil
	}
	out := s.modeSeq[0]
	if len(s.modeSeq) > 1 {
		s.modeSeq = s.modeSeq[1:]
	}
	return out, nil
}

func (s *mbimRegistrationTestController) SetOperatingMode(_ context.Context, mode backend.OperatingMode) error {
	s.setModes = append(s.setModes, mode)
	return nil
}

func (s *mbimRegistrationTestController) SetOperatorSelection(_ context.Context, req backend.SetOperatorSelectionRequest) (backend.OperatorSelection, error) {
	s.selections = append(s.selections, req)
	return backend.OperatorSelection{Mode: req.Mode, PLMN: req.PLMN}, nil
}

func (s *mbimRegistrationTestController) AttachPacketService(context.Context) error {
	s.attachCalls++
	return nil
}

func (s *mbimRegistrationTestController) IsSimInserted(context.Context) (bool, error) {
	if len(s.simInsertedSeq) == 0 {
		return true, nil
	}
	out := s.simInsertedSeq[0]
	if len(s.simInsertedSeq) > 1 {
		s.simInsertedSeq = s.simInsertedSeq[1:]
	}
	return out, nil
}

func TestEnsureMBIMRegistrationAttachesWhenRegisteredWithoutPacketService(t *testing.T) {
	ctrl := &mbimRegistrationTestController{
		servingSeq: []*backend.ServingSystem{
			{RegStatus: 1, RegStatusText: "registered-home", PSAttached: false},
			{RegStatus: 1, RegStatusText: "registered-home", PSAttached: true},
		},
	}
	err := ensureMBIMRegistration(context.Background(), "dev-mbim", config.DeviceConfig{}, ctrl, mbimRegistrationOptions{
		PollInterval: time.Millisecond,
		MaxAttempts:  3,
	})
	if err != nil {
		t.Fatalf("ensureMBIMRegistration() error=%v", err)
	}
	if ctrl.attachCalls != 1 {
		t.Fatalf("attachCalls=%d want 1", ctrl.attachCalls)
	}
}

func TestEnsureMBIMRegistrationSuppressionPreventsRadioSideEffects(t *testing.T) {
	ctrl := &mbimRegistrationTestController{modeSeq: []backend.OperatingMode{backend.ModeRFOff}}
	err := ensureMBIMRegistration(context.Background(), "dev-mbim", config.DeviceConfig{}, ctrl, mbimRegistrationOptions{
		PollInterval: time.Nanosecond,
		MaxAttempts:  1,
		ShouldAbort:  func() bool { return true },
	})
	if !errors.Is(err, errQMIRegistrationSkipped) {
		t.Fatalf("ensureMBIMRegistration() error=%v want %v", err, errQMIRegistrationSkipped)
	}
	if len(ctrl.setModes) != 0 || len(ctrl.selections) != 0 || ctrl.attachCalls != 0 {
		t.Fatalf("suppressed registration produced side effects: modes=%v selections=%v attach=%d",
			ctrl.setModes, ctrl.selections, ctrl.attachCalls)
	}
}

func TestEnsureMBIMRegistrationSubmitsManualSelectionWhenSearching(t *testing.T) {
	ctrl := &mbimRegistrationTestController{
		servingSeq: []*backend.ServingSystem{
			{RegStatus: 2, RegStatusText: "searching"},
			{RegStatus: 5, RegStatusText: "registered-roaming", PSAttached: true},
		},
	}
	cfg := config.DeviceConfig{
		OperatorSelectionMode: "manual",
		OperatorSelectionPLMN: "310260",
		OperatorSelectionRAT:  string(backend.OperatorRATLTE),
	}
	err := ensureMBIMRegistration(context.Background(), "dev-mbim", cfg, ctrl, mbimRegistrationOptions{
		PollInterval: time.Millisecond,
		MaxAttempts:  3,
	})
	if err != nil {
		t.Fatalf("ensureMBIMRegistration() error=%v", err)
	}
	if len(ctrl.selections) != 1 || ctrl.selections[0].Mode != backend.OperatorSelectionManual || ctrl.selections[0].PLMN != "310260" {
		t.Fatalf("selections=%+v want one manual selection", ctrl.selections)
	}
}

func TestEnsureMBIMRegistrationRadioCyclesAfterPersistentSearching(t *testing.T) {
	ctrl := &mbimRegistrationTestController{
		servingSeq: []*backend.ServingSystem{
			{RegStatus: 2, RegStatusText: "searching"},
			{RegStatus: 2, RegStatusText: "searching"},
			{RegStatus: 5, RegStatusText: "registered-roaming", PSAttached: true},
		},
	}
	err := ensureMBIMRegistration(context.Background(), "dev-mbim", config.DeviceConfig{}, ctrl, mbimRegistrationOptions{
		PollInterval:            time.Millisecond,
		MaxAttempts:             3,
		RadioCycleAfterAttempts: 2,
	})
	if err != nil {
		t.Fatalf("ensureMBIMRegistration() error=%v", err)
	}
	if len(ctrl.setModes) != 2 || ctrl.setModes[0] != backend.ModeRFOff || ctrl.setModes[1] != backend.ModeOnline {
		t.Fatalf("setModes=%+v want RFOff then Online", ctrl.setModes)
	}
}

func TestEnsureMBIMRegistrationReturnsDenied(t *testing.T) {
	ctrl := &mbimRegistrationTestController{
		servingSeq: []*backend.ServingSystem{{RegStatus: 3, RegStatusText: "denied"}},
	}
	err := ensureMBIMRegistration(context.Background(), "dev-mbim", config.DeviceConfig{}, ctrl, mbimRegistrationOptions{
		PollInterval: time.Millisecond,
		MaxAttempts:  1,
	})
	if !errors.Is(err, errMBIMRegistrationDenied) {
		t.Fatalf("error=%v want errMBIMRegistrationDenied", err)
	}
}
