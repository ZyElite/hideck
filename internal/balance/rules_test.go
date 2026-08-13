package balance

import (
	"context"
	"errors"
	"testing"

	"github.com/iniwex5/vohive/internal/carrierquery"
)

type customRuleFixture []carrierquery.Rule

func (f customRuleFixture) ListCustomCarrierQueryRules() ([]carrierquery.Rule, error) {
	return append([]carrierquery.Rule(nil), f...), nil
}

func TestRulesTreatSameIDCustomRuleAsCompleteBuiltInOverride(t *testing.T) {
	builtIn := carrierquery.BuiltInRules()[0]
	override := builtIn
	override.BuiltIn = false
	override.MCC = "999"
	override.MNC = "99"

	rules := NewRules(customRuleFixture{override})
	if _, err := rules.Resolve(context.Background(), DeviceSnapshot{MCC: builtIn.MCC, MNC: builtIn.MNC}); !errors.Is(err, ErrRuleNotFound) {
		t.Fatalf("Resolve(original PLMN) error = %v, want ErrRuleNotFound", err)
	}
	got, err := rules.Resolve(context.Background(), DeviceSnapshot{MCC: override.MCC, MNC: override.MNC})
	if err != nil || got.ID != override.ID || got.BuiltIn {
		t.Fatalf("Resolve(override PLMN) = %+v, %v", got, err)
	}
}

func TestRulesDoNotFallBackToDisabledBuiltInOverride(t *testing.T) {
	override := carrierquery.BuiltInRules()[0]
	override.BuiltIn = false
	override.Enabled = false
	rules := NewRules(customRuleFixture{override})

	if _, err := rules.Resolve(context.Background(), DeviceSnapshot{MCC: override.MCC, MNC: override.MNC}); !errors.Is(err, ErrRuleNotFound) {
		t.Fatalf("Resolve() error = %v, want ErrRuleNotFound", err)
	}
	if _, err := rules.ByID(context.Background(), override.ID); !errors.Is(err, ErrRuleNotFound) {
		t.Fatalf("ByID() error = %v, want ErrRuleNotFound", err)
	}

	restored, err := NewRules(customRuleFixture{}).ByID(context.Background(), override.ID)
	if err != nil || restored.ID != override.ID || !restored.BuiltIn {
		t.Fatalf("ByID() after delete = %+v, %v", restored, err)
	}
}

func TestRulesPreferMatchingSPNCustomOverride(t *testing.T) {
	custom := carrierquery.Rule{ID: "custom-giffgaff", MCC: "234", MNC: "010", SPN: "giffgaff", Operator: "Custom",
		Transport: carrierquery.TransportSMS, Destination: "1", Payload: "BAL", ResponseMode: carrierquery.ResponseSMS, Enabled: true}
	rules := NewRules(customRuleFixture{custom})
	got, err := rules.Resolve(context.Background(), DeviceSnapshot{MCC: "234", MNC: "10", SPN: "GiffGaff"})
	if err != nil || got.ID != custom.ID {
		t.Fatalf("Resolve() = %+v, %v", got, err)
	}
	got, err = rules.Resolve(context.Background(), DeviceSnapshot{MCC: "234", MNC: "10", SPN: "other"})
	if err != nil || got.ID != "giffgaff_23410" {
		t.Fatalf("Resolve(fallback) = %+v, %v", got, err)
	}
}
