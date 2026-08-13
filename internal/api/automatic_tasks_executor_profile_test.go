package api

import (
	"strings"
	"testing"

	"github.com/iniwex5/vohive/internal/automation"
)

func TestResolveAutomaticTaskProfileUsesCurrentPhysicalSIM(t *testing.T) {
	task := automation.Task{ProfileICCID: "8944101234567890123"}
	action, err := resolveAutomaticTaskProfile(task, "\"8944101234567890123F\"")
	if err != nil {
		t.Fatal(err)
	}
	if action != automaticTaskUseCurrentSIM {
		t.Fatalf("action=%d want current SIM", action)
	}
}

func TestResolveAutomaticTaskProfileSwitchesOnlyWithAID(t *testing.T) {
	task := automation.Task{
		ProfileICCID: "8944101234567890123",
		ProfileAID:   "A0000005591010FFFFFFFF8900000100",
	}
	action, err := resolveAutomaticTaskProfile(task, "8944100000000000000")
	if err != nil {
		t.Fatal(err)
	}
	if action != automaticTaskSwitchESIM {
		t.Fatalf("action=%d want eSIM switch", action)
	}
}

func TestResolveAutomaticTaskProfileRejectsUnverifiablePhysicalSIM(t *testing.T) {
	tests := []struct {
		name    string
		current string
		wantErr string
	}{
		{name: "identity unavailable", wantErr: "current SIM ICCID is unavailable"},
		{name: "different card", current: "8944100000000000000", wantErr: "eSIM AID is required"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			task := automation.Task{ProfileICCID: "8944101234567890123"}
			_, err := resolveAutomaticTaskProfile(task, test.current)
			if err == nil || !strings.Contains(err.Error(), test.wantErr) {
				t.Fatalf("error=%v want containing %q", err, test.wantErr)
			}
		})
	}
}
