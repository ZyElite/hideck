package policy

import (
	"reflect"
	"strings"
)

func defaultIMSRegisterSecurityClientMechanismsCopy() []IPSec3GPPSecurityMechanism {
	return cloneMechanisms(defaultSecurityMechanisms)
}

func defaultIMSRegisterStatusCodeListCopy(values []int) []int { return cloneInts(values) }

func defaultContactParamOrder() []string { return cloneStrings(defaultContactParams) }

func registerPolicyEquals(left, right IMSRegisterPolicy) bool {
	return left.ID == right.ID && left.TemporaryRetrySeconds == right.TemporaryRetrySeconds &&
		reflect.DeepEqual(left.TemporaryStatusCodes, right.TemporaryStatusCodes) &&
		reflect.DeepEqual(left.ForbiddenStatusCodes, right.ForbiddenStatusCodes) &&
		reflect.DeepEqual(left.InitialRejectFallbackStatusCodes, right.InitialRejectFallbackStatusCodes)
}

func trimStringFields(target any) {
	value := reflect.ValueOf(target)
	if value.Kind() != reflect.Pointer || value.IsNil() {
		return
	}
	value = value.Elem()
	if value.Kind() != reflect.Struct {
		return
	}
	for index := 0; index < value.NumField(); index++ {
		field := value.Field(index)
		if field.Kind() == reflect.String && field.CanSet() {
			field.SetString(strings.TrimSpace(field.String()))
		}
	}
}

func applyOverrideByFieldName(target, override any) {
	targetValue, overrideValue := indirectStruct(target), indirectStruct(override)
	if !targetValue.IsValid() || !overrideValue.IsValid() {
		return
	}
	for index := 0; index < overrideValue.NumField(); index++ {
		source := overrideValue.Field(index)
		destination := targetValue.FieldByName(overrideValue.Type().Field(index).Name)
		if !destination.IsValid() || !destination.CanSet() {
			continue
		}
		applyReflectedOverride(destination, source)
	}
}

func indirectStruct(value any) reflect.Value {
	result := reflect.ValueOf(value)
	if result.Kind() != reflect.Pointer || result.IsNil() {
		return reflect.Value{}
	}
	result = result.Elem()
	if result.Kind() != reflect.Struct {
		return reflect.Value{}
	}
	return result
}

func applyReflectedOverride(destination, source reflect.Value) {
	switch source.Kind() {
	case reflect.Pointer:
		if source.IsNil() {
			return
		}
		if destination.Kind() == reflect.Pointer {
			copy := reflect.New(source.Elem().Type())
			copy.Elem().Set(source.Elem())
			destination.Set(copy)
		} else if source.Elem().Type().AssignableTo(destination.Type()) {
			destination.Set(source.Elem())
		}
	case reflect.Slice:
		if source.Len() > 0 && source.Type().AssignableTo(destination.Type()) {
			copy := reflect.MakeSlice(source.Type(), source.Len(), source.Len())
			reflect.Copy(copy, source)
			destination.Set(copy)
		}
	case reflect.String:
		if source.Len() > 0 && source.Type().AssignableTo(destination.Type()) {
			destination.Set(source)
		}
	}
}
