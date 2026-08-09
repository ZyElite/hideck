package profile

import "testing"

func TestNormalizeAKAApp(t *testing.T) {
	tests := map[string]string{
		" auto ":      AKAAppAuto,
		"ISIM":        AKAAppISIM,
		"isim_strict": AKAAppISIMStrict,
		"usim":        AKAAppUSIM,
		"usim_strict": AKAAppUSIM,
		"":            AKAAppUSIM,
	}
	for input, want := range tests {
		if got := NormalizeAKAApp(input); got != want {
			t.Errorf("NormalizeAKAApp(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestAuthPlanAndPreparedSession(t *testing.T) {
	if !(AuthPlan{}).IsZero() {
		t.Fatal("empty AuthPlan is not zero")
	}
	plan := NewAuthPlan("ISIM", "auto")
	if plan.EPDGApp != AKAAppISIM || plan.IMSApp != AKAAppAuto || plan.IsZero() {
		t.Fatalf("NewAuthPlan() = %+v", plan)
	}

	defaulted := (PreparedSession{IMSIdentity: IMSIdentityResult{
		AKAAppPreference: AKAAppISIMStrict,
	}}).EffectiveAuthPlan()
	if defaulted.EPDGApp != AKAAppUSIM || defaulted.IMSApp != AKAAppISIMStrict {
		t.Fatalf("EffectiveAuthPlan() = %+v", defaulted)
	}
}
