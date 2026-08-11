package balance

import (
	"context"
	"testing"

	"github.com/iniwex5/vohive/internal/carrierquery"
)

type customRuleFixture []carrierquery.Rule

func (f customRuleFixture) ListCustomCarrierQueryRules() ([]carrierquery.Rule, error) {
	return append([]carrierquery.Rule(nil), f...), nil
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
