package esim

import (
	"fmt"
	"testing"

	sgp22 "github.com/damonto/euicc-go/v2"
)

func TestClassifyDisableProfileOperationResult(t *testing.T) {
	cases := []struct {
		result sgp22.ProfileOperationResult
		want   DisableProfileErrorCode
	}{
		{sgp22.ProfileOperationResultICCIDOrAIDNotFound, DisableProfileErrorProfileNotFound},
		{sgp22.ProfileOperationResultProfileNotInEnabledState, DisableProfileErrorProfileNotEnabled},
		{sgp22.ProfileOperationResultDisallowedByPolicy, DisableProfileErrorDisallowedByPolicy},
		{sgp22.ProfileOperationResultCATBusy, DisableProfileErrorCATBusy},
		{sgp22.ProfileOperationResultUndefinedError, DisableProfileErrorInternal},
	}

	for _, tc := range cases {
		err := fmt.Errorf("disable failed: %w", &sgp22.ProfileOperationError{
			Operation: sgp22.DisableProfile,
			Result:    tc.result,
		})
		if got := ClassifyDisableProfileError(err); got != tc.want {
			t.Fatalf("result=%v got=%q want=%q", tc.result, got, tc.want)
		}
	}
}

func TestClassifySwitchProfileOperationResult(t *testing.T) {
	cases := []struct {
		result sgp22.ProfileOperationResult
		want   SwitchProfileErrorCode
	}{
		{sgp22.ProfileOperationResultICCIDOrAIDNotFound, SwitchProfileErrorProfileNotFound},
		{sgp22.ProfileOperationResultProfileNotInDisabledState, SwitchProfileErrorProfileNotDisabled},
		{sgp22.ProfileOperationResultDisallowedByPolicy, SwitchProfileErrorPolicyRejected},
		{sgp22.ProfileOperationResultWrongProfileReenabling, SwitchProfileErrorWrongReenable},
		{sgp22.ProfileOperationResultCATBusy, SwitchProfileErrorCATBusy},
		{sgp22.ProfileOperationResultUndefinedError, SwitchProfileErrorInternal},
	}

	for _, tc := range cases {
		err := fmt.Errorf("switch failed: %w", &sgp22.ProfileOperationError{
			Operation: sgp22.EnableProfile,
			Result:    tc.result,
		})
		if got := ClassifySwitchProfileError(err); got != tc.want {
			t.Fatalf("result=%v got=%q want=%q", tc.result, got, tc.want)
		}
	}
}

func TestClassifyDisableProfileIgnoresOtherOperations(t *testing.T) {
	err := &sgp22.ProfileOperationError{
		Operation: sgp22.EnableProfile,
		Result:    sgp22.ProfileOperationResultDisallowedByPolicy,
	}
	if got := ClassifyDisableProfileError(err); got != DisableProfileErrorInternal {
		t.Fatalf("got=%q want=%q", got, DisableProfileErrorInternal)
	}
}

func TestClassifyDeleteProfileOperationResult(t *testing.T) {
	cases := []struct {
		result sgp22.ProfileOperationResult
		want   DeleteProfileErrorCode
	}{
		{sgp22.ProfileOperationResultICCIDOrAIDNotFound, DeleteProfileErrorProfileNotFound},
		{sgp22.ProfileOperationResultProfileNotInDisabledState, DeleteProfileErrorProfileEnabled},
		{sgp22.ProfileOperationResultDisallowedByPolicy, DeleteProfileErrorPolicyRejected},
		{sgp22.ProfileOperationResultUndefinedError, DeleteProfileErrorInternal},
	}

	for _, tc := range cases {
		err := fmt.Errorf("delete failed: %w", &sgp22.ProfileOperationError{
			Operation: sgp22.DeleteProfile,
			Result:    tc.result,
		})
		if got := ClassifyDeleteProfileError(err); got != tc.want {
			t.Fatalf("result=%v got=%q want=%q", tc.result, got, tc.want)
		}
	}
}
