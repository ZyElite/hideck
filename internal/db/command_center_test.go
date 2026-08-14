package db

import (
	"path/filepath"
	"testing"

	"github.com/yibaiba/hideck/internal/carrierquery"
)

func TestCommandCenterSchemaAndCustomRuleRoundTrip(t *testing.T) {
	if err := Init(filepath.Join(t.TempDir(), "command-center.db")); err != nil {
		t.Fatalf("Init() error = %v", err)
	}
	t.Cleanup(func() { DB = nil })

	for _, model := range []any{&CommandExecution{}, &CommandEvent{}, &BalanceQuery{}, &CustomCarrierQueryRule{}} {
		if !DB.Migrator().HasTable(model) {
			t.Fatalf("missing migrated table for %T", model)
		}
	}
	if !DB.Migrator().HasColumn(&BalanceQuery{}, "iccid") {
		t.Fatal("balance_queries is missing canonical iccid column")
	}

	rule := carrierquery.Rule{ID: "custom-balance", MCC: "234", MNC: "10", Operator: "Custom",
		Transport: carrierquery.TransportSMS, Destination: "123", Payload: "BAL",
		ResponseMode: carrierquery.ResponseSMS, ExpectedSenders: []string{"123"},
		ParserPattern: `(?P<amount>[0-9]+)`, Currency: "GBP", CostStatus: "unknown",
		EvidenceType: "custom", Enabled: true}
	if err := SaveCustomCarrierQueryRule(rule); err != nil {
		t.Fatalf("SaveCustomCarrierQueryRule() error = %v", err)
	}
	stored, err := GetCustomCarrierQueryRule(rule.ID)
	if err != nil || stored == nil {
		t.Fatalf("GetCustomCarrierQueryRule() = %#v, %v", stored, err)
	}
	if stored.ParserPattern != rule.ParserPattern || len(stored.ExpectedSenders) != 1 {
		t.Fatalf("stored rule = %#v", stored)
	}
	if err := DeleteCustomCarrierQueryRule(rule.ID); err != nil {
		t.Fatalf("DeleteCustomCarrierQueryRule() error = %v", err)
	}
	stored, err = GetCustomCarrierQueryRule(rule.ID)
	if err != nil || stored != nil {
		t.Fatalf("GetCustomCarrierQueryRule() after delete = %#v, %v", stored, err)
	}
}

func TestSaveCustomCarrierQueryRuleRejectsInvalidRE2(t *testing.T) {
	if err := Init(filepath.Join(t.TempDir(), "invalid-rule.db")); err != nil {
		t.Fatalf("Init() error = %v", err)
	}
	t.Cleanup(func() { DB = nil })
	rule := carrierquery.Rule{ID: "invalid", MCC: "234", MNC: "10", Operator: "Custom",
		Transport: carrierquery.TransportUSSD, Payload: "*100#", ResponseMode: carrierquery.ResponseDirect,
		ParserPattern: "(", CostStatus: "unknown", EvidenceType: "custom", Enabled: true}
	if err := SaveCustomCarrierQueryRule(rule); err == nil {
		t.Fatal("SaveCustomCarrierQueryRule() accepted invalid parser")
	}
}
