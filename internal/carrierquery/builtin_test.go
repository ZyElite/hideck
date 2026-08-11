package carrierquery

import "testing"

func TestBuiltInRulesCoverProductionPresets(t *testing.T) {
	expected := []struct{ mcc, mnc, id string }{
		{"530", "24", "2degrees_nz_53024"}, {"310", "280", "att_310280"},
		{"310", "410", "att_310410"}, {"454", "000", "csl_454000"},
		{"234", "33", "cteuk_23433"}, {"234", "10", "giffgaff_23410"},
		{"262", "03", "o2_de_26203"}, {"262", "07", "o2_de_26207"},
		{"530", "01", "one_nz_53001"}, {"530", "05", "spark_nz_53005"},
		{"228", "002", "sunrise_22802"}, {"454", "003", "three_hk_454003"},
		{"234", "020", "three_uk_234020"}, {"310", "240", "tmobile_310240"},
		{"310", "260", "tmobile_310260"}, {"204", "04", "vodafone_nl_20404"},
	}

	rules := BuiltInRules()
	if len(rules) != len(expected) {
		t.Fatalf("BuiltInRules() count = %d, want %d", len(rules), len(expected))
	}
	for _, item := range expected {
		rule, ok := FindBuiltIn(item.mcc, item.mnc)
		if !ok || rule.ID != item.id {
			t.Errorf("FindBuiltIn(%s, %s) = %q/%v, want %q/true", item.mcc, item.mnc, rule.ID, ok, item.id)
			continue
		}
		if err := rule.Validate(); err != nil {
			t.Errorf("rule %s invalid: %v", rule.ID, err)
		}
	}
}

func TestBuiltInRulesReturnDefensiveCopies(t *testing.T) {
	rules := BuiltInRules()
	rules[0].ExpectedSenders[0] = "changed"
	rule, _ := FindBuiltIn("530", "24")
	if rule.ExpectedSenders[0] == "changed" {
		t.Fatal("BuiltInRules() exposed mutable registry state")
	}
}

func TestRuleValidationRejectsInvalidParser(t *testing.T) {
	rule := BuiltInRules()[0]
	rule.ID = "custom-rule"
	rule.BuiltIn = false
	rule.ParserPattern = "("
	if err := rule.Validate(); err == nil {
		t.Fatal("Validate() accepted invalid RE2 pattern")
	}
}

func TestRuleValidationRejectsSMSReplyWithoutExpectedSender(t *testing.T) {
	rule := BuiltInRules()[0]
	rule.ID = "custom-rule"
	rule.BuiltIn = false
	rule.ExpectedSenders = []string{"", "  "}
	if err := rule.Validate(); err == nil {
		t.Fatal("Validate() accepted a reply rule that cannot correlate inbound SMS")
	}
}
