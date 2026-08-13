package esim

import (
	"errors"

	sgp22 "github.com/damonto/euicc-go/v2"
)

type SwitchProfileErrorCode string

const (
	SwitchProfileErrorInvalidICCID       SwitchProfileErrorCode = "INVALID_ICCID"
	SwitchProfileErrorInvalidAIDHex      SwitchProfileErrorCode = "INVALID_AID_HEX"
	SwitchProfileErrorProfileNotFound    SwitchProfileErrorCode = "PROFILE_NOT_FOUND"
	SwitchProfileErrorProfileNotDisabled SwitchProfileErrorCode = "PROFILE_NOT_DISABLED"
	SwitchProfileErrorPolicyRejected     SwitchProfileErrorCode = "DISALLOWED_BY_POLICY"
	SwitchProfileErrorWrongReenable      SwitchProfileErrorCode = "WRONG_PROFILE_REENABLING"
	SwitchProfileErrorCATBusy            SwitchProfileErrorCode = "CAT_BUSY"
	SwitchProfileErrorInternal           SwitchProfileErrorCode = "INTERNAL"
)

type SwitchProfileError struct {
	Code    SwitchProfileErrorCode
	Message string
	Err     error
}

func (e *SwitchProfileError) Error() string {
	if e == nil {
		return ""
	}
	if e.Message != "" {
		return e.Message
	}
	if e.Err != nil {
		return e.Err.Error()
	}
	return "switch profile failed"
}

func (e *SwitchProfileError) Unwrap() error { return e.Err }

func NewSwitchProfileError(code SwitchProfileErrorCode, message string, err error) error {
	return &SwitchProfileError{Code: code, Message: message, Err: err}
}

func ClassifySwitchProfileError(err error) SwitchProfileErrorCode {
	if err == nil {
		return ""
	}
	var switchErr *SwitchProfileError
	if errors.As(err, &switchErr) && switchErr.Code != "" {
		return switchErr.Code
	}
	var operationErr *sgp22.ProfileOperationError
	if errors.As(err, &operationErr) && operationErr.Operation == sgp22.EnableProfile {
		return classifySwitchProfileOperationResult(operationErr.Result)
	}
	return SwitchProfileErrorInternal
}

func classifySwitchProfileOperationResult(result sgp22.ProfileOperationResult) SwitchProfileErrorCode {
	switch result {
	case sgp22.ProfileOperationResultICCIDOrAIDNotFound:
		return SwitchProfileErrorProfileNotFound
	case sgp22.ProfileOperationResultProfileNotInDisabledState:
		return SwitchProfileErrorProfileNotDisabled
	case sgp22.ProfileOperationResultDisallowedByPolicy:
		return SwitchProfileErrorPolicyRejected
	case sgp22.ProfileOperationResultWrongProfileReenabling:
		return SwitchProfileErrorWrongReenable
	case sgp22.ProfileOperationResultCATBusy:
		return SwitchProfileErrorCATBusy
	default:
		return SwitchProfileErrorInternal
	}
}

// DisableProfileErrorCode 表示停用 Profile 时可由调用方处理的失败类别。
type DisableProfileErrorCode string

const (
	DisableProfileErrorInvalidICCID       DisableProfileErrorCode = "INVALID_ICCID"
	DisableProfileErrorInvalidAIDHex      DisableProfileErrorCode = "INVALID_AID_HEX"
	DisableProfileErrorProfileNotFound    DisableProfileErrorCode = "PROFILE_NOT_FOUND"
	DisableProfileErrorProfileNotEnabled  DisableProfileErrorCode = "PROFILE_NOT_ENABLED"
	DisableProfileErrorDisallowedByPolicy DisableProfileErrorCode = "DISALLOWED_BY_POLICY"
	DisableProfileErrorCATBusy            DisableProfileErrorCode = "CAT_BUSY"
	DisableProfileErrorBusy               DisableProfileErrorCode = "BUSY"
	DisableProfileErrorInternal           DisableProfileErrorCode = "INTERNAL"
)

type DisableProfileError struct {
	Code    DisableProfileErrorCode
	Message string
	Err     error
}

func (e *DisableProfileError) Error() string {
	if e == nil {
		return ""
	}
	if e.Message != "" {
		return e.Message
	}
	if e.Err != nil {
		return e.Err.Error()
	}
	return "disable profile failed"
}

func (e *DisableProfileError) Unwrap() error { return e.Err }

func NewDisableProfileError(code DisableProfileErrorCode, message string, err error) error {
	return &DisableProfileError{Code: code, Message: message, Err: err}
}

func ClassifyDisableProfileError(err error) DisableProfileErrorCode {
	if err == nil {
		return ""
	}
	if errors.Is(err, ErrOperationInProgress) {
		return DisableProfileErrorBusy
	}
	var disableErr *DisableProfileError
	if errors.As(err, &disableErr) && disableErr.Code != "" {
		return disableErr.Code
	}
	var operationErr *sgp22.ProfileOperationError
	if errors.As(err, &operationErr) && operationErr.Operation == sgp22.DisableProfile {
		return classifyDisableProfileOperationResult(operationErr.Result)
	}
	if errors.Is(err, sgp22.ErrCatBusy) {
		return DisableProfileErrorCATBusy
	}
	return DisableProfileErrorInternal
}

func classifyDisableProfileOperationResult(result sgp22.ProfileOperationResult) DisableProfileErrorCode {
	switch result {
	case sgp22.ProfileOperationResultICCIDOrAIDNotFound:
		return DisableProfileErrorProfileNotFound
	case sgp22.ProfileOperationResultProfileNotInEnabledState:
		return DisableProfileErrorProfileNotEnabled
	case sgp22.ProfileOperationResultDisallowedByPolicy:
		return DisableProfileErrorDisallowedByPolicy
	case sgp22.ProfileOperationResultCATBusy:
		return DisableProfileErrorCATBusy
	default:
		return DisableProfileErrorInternal
	}
}

func IsDisableProfileBusy(err error) bool {
	return ClassifyDisableProfileError(err) == DisableProfileErrorBusy
}

// DeleteProfileErrorCode 表示 DeleteProfile 场景的可判别错误类别。
type DeleteProfileErrorCode string

const (
	DeleteProfileErrorInvalidICCID    DeleteProfileErrorCode = "INVALID_ICCID"
	DeleteProfileErrorInvalidAIDHex   DeleteProfileErrorCode = "INVALID_AID_HEX"
	DeleteProfileErrorProfileNotFound DeleteProfileErrorCode = "PROFILE_NOT_FOUND"
	DeleteProfileErrorEUICCNotFound   DeleteProfileErrorCode = "EUICC_NOT_FOUND"
	DeleteProfileErrorProfileEnabled  DeleteProfileErrorCode = "PROFILE_ENABLED"
	DeleteProfileErrorPolicyRejected  DeleteProfileErrorCode = "DISALLOWED_BY_POLICY"
	DeleteProfileErrorBusy            DeleteProfileErrorCode = "BUSY"
	DeleteProfileErrorInternal        DeleteProfileErrorCode = "INTERNAL"
)

// DeleteProfileError 为删除 profile 提供结构化错误，便于 API 做稳定状态码映射。
type DeleteProfileError struct {
	Code    DeleteProfileErrorCode
	Message string
	Err     error
}

func (e *DeleteProfileError) Error() string {
	if e == nil {
		return ""
	}
	if e.Message != "" {
		return e.Message
	}
	if e.Err != nil {
		return e.Err.Error()
	}
	return "delete profile failed"
}

func (e *DeleteProfileError) Unwrap() error { return e.Err }

// NewDeleteProfileError 构造一个可判别的 DeleteProfileError。
func NewDeleteProfileError(code DeleteProfileErrorCode, message string, err error) error {
	return &DeleteProfileError{
		Code:    code,
		Message: message,
		Err:     err,
	}
}

// ClassifyDeleteProfileError 返回 DeleteProfile 错误类别。
func ClassifyDeleteProfileError(err error) DeleteProfileErrorCode {
	if err == nil {
		return ""
	}
	if errors.Is(err, ErrOperationInProgress) {
		return DeleteProfileErrorBusy
	}
	var de *DeleteProfileError
	if errors.As(err, &de) && de.Code != "" {
		return de.Code
	}
	var operationErr *sgp22.ProfileOperationError
	if errors.As(err, &operationErr) && operationErr.Operation == sgp22.DeleteProfile {
		switch operationErr.Result {
		case sgp22.ProfileOperationResultICCIDOrAIDNotFound:
			return DeleteProfileErrorProfileNotFound
		case sgp22.ProfileOperationResultProfileNotInDisabledState:
			return DeleteProfileErrorProfileEnabled
		case sgp22.ProfileOperationResultDisallowedByPolicy:
			return DeleteProfileErrorPolicyRejected
		}
	}
	return DeleteProfileErrorInternal
}

func IsDeleteProfileInvalidInput(err error) bool {
	switch ClassifyDeleteProfileError(err) {
	case DeleteProfileErrorInvalidICCID, DeleteProfileErrorInvalidAIDHex:
		return true
	default:
		return false
	}
}

func IsDeleteProfileNotFound(err error) bool {
	switch ClassifyDeleteProfileError(err) {
	case DeleteProfileErrorProfileNotFound, DeleteProfileErrorEUICCNotFound:
		return true
	default:
		return false
	}
}

func IsDeleteProfileBusy(err error) bool {
	return ClassifyDeleteProfileError(err) == DeleteProfileErrorBusy
}

func IsDeleteProfileConflict(err error) bool {
	switch ClassifyDeleteProfileError(err) {
	case DeleteProfileErrorBusy, DeleteProfileErrorProfileEnabled, DeleteProfileErrorPolicyRejected:
		return true
	default:
		return false
	}
}
