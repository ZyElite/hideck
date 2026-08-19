package device

import (
	"errors"
	"testing"
)

func TestClassifyLebaraUKNextGen(t *testing.T) {
	tests := []struct {
		name, imsi, profile string
		seen                []string
		wantLebara          bool
		wantHome            bool
		wantFlipped         bool
		wantBlock           bool
	}{
		{name: "live 23487", imsi: "234870000000001", wantLebara: true, wantHome: true},
		{name: "profile Lebara UK", imsi: "204040000000001", profile: "Lebara UK", wantLebara: true, wantFlipped: true, wantBlock: true},
		{name: "profile 0 Lebara UK", imsi: "204040000000001", profile: "0 Lebara UK", wantLebara: true, wantFlipped: true, wantBlock: true},
		{name: "history 23487", imsi: "204040000000001", seen: []string{"234870000000001"}, wantLebara: true, wantFlipped: true, wantBlock: true},
		{name: "bare 20404 is NL", imsi: "204040000000001"},
		{name: "voxi stays voxi", imsi: "234150000000001", profile: "VOXI"},
		{name: "lebara nl name ignored", imsi: "204040000000001", profile: "Lebara NL"},
		{name: "empty imsi name only does not block vowifi", profile: "Lebara UK", wantLebara: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ClassifyLebaraUKNextGen(tt.imsi, tt.profile, tt.seen)
			if got.IsLebara != tt.wantLebara || got.LiveHome23487 != tt.wantHome ||
				got.LiveFlipped != tt.wantFlipped || got.BlocksVoWiFi() != tt.wantBlock {
				t.Fatalf("got %+v block=%v", got, got.BlocksVoWiFi())
			}
			if tt.wantLebara && got.RFLock() != RFLockLebaraUKNextGen {
				t.Fatalf("RFLock = %q", got.RFLock())
			}
			if !tt.wantLebara && got.RFLock() != "" {
				t.Fatalf("unexpected RFLock %q", got.RFLock())
			}
		})
	}
}

func TestLebaraUKPolicyErrors(t *testing.T) {
	if !IsLebaraUKPolicyError(ErrLebaraUKRFLocked) || !IsLebaraUKPolicyError(NewLebaraUKFlippedIMSIError("20404")) {
		t.Fatal("policy errors should match")
	}
	if IsLebaraUKPolicyError(errors.New("other")) {
		t.Fatal("unrelated error matched")
	}
	if err := NewLebaraUKFlippedIMSIError("204040000000001"); !errors.Is(err, ErrLebaraUKFlippedIMSI) {
		t.Fatalf("wrap = %v", err)
	}
}
