package messaging

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"
)

type recoveredField struct {
	name   string
	typeOf reflect.Type
	json   string
}

func assertRecoveredPrefix(t *testing.T, value any, fields []recoveredField) {
	t.Helper()
	typeOf := reflect.TypeOf(value)
	if typeOf.NumField() < len(fields) {
		t.Fatalf("%s has %d fields, want at least %d", typeOf, typeOf.NumField(), len(fields))
	}
	for index, want := range fields {
		got := typeOf.Field(index)
		if got.Name != want.name || got.Type != want.typeOf || got.Tag.Get("json") != want.json {
			t.Fatalf("%s field %d = %s %s json=%q, want %s %s json=%q",
				typeOf, index, got.Name, got.Type, got.Tag.Get("json"),
				want.name, want.typeOf, want.json)
		}
	}
}

func TestRecoveredMessagingTypePrefixes(t *testing.T) {
	stringType := reflect.TypeOf("")
	intType := reflect.TypeOf(0)
	timeType := reflect.TypeOf(time.Time{})
	assertRecoveredPrefix(t, SendOptions{}, []recoveredField{
		{"Encoding", stringType, "encoding,omitempty"},
	})
	assertRecoveredPrefix(t, SendOutcome{}, []recoveredField{
		{"MessageID", stringType, "message_id"},
		{"PartsTotal", intType, "parts_total"},
		{"DeliveryState", stringType, "delivery_state"},
	})
	assertRecoveredPrefix(t, USSDResult{}, []recoveredField{
		{"Status", intType, "status"}, {"Text", stringType, "text"},
		{"RawXML", stringType, "raw_xml,omitempty"}, {"DCS", intType, "dcs"},
		{"SessionID", stringType, "session_id,omitempty"},
	})
	assertRecoveredPrefix(t, ServiceStatus{}, []recoveredField{
		{"Enabled", reflect.TypeOf(false), ""}, {"DeviceID", stringType, ""},
		{"Registered", reflect.TypeOf(false), ""}, {"RegStatus", stringType, ""},
		{"Registrar", stringType, ""}, {"LocalAddr", stringType, ""},
		{"AssociatedMSISDN", stringType, ""}, {"LastSIPCode", intType, ""},
		{"LastSIPText", stringType, ""}, {"PingFailCount", intType, ""},
		{"LastSMSAt", timeType, ""}, {"LastSMSError", stringType, ""},
	})
}

func TestRecoveredDeliveryTypePrefixes(t *testing.T) {
	stringType := reflect.TypeOf("")
	intType := reflect.TypeOf(0)
	timeType := reflect.TypeOf(time.Time{})
	assertRecoveredPrefix(t, DeliveryPartMatch{}, []recoveredField{
		{"MessageID", stringType, ""}, {"PartNo", intType, ""}, {"State", stringType, ""},
	})
	assertRecoveredPrefix(t, DeliveryPartStatus{}, []recoveredField{
		{"PartNo", intType, "part_no"}, {"CallID", stringType, "call_id"},
		{"InReplyTo", stringType, "in_reply_to"}, {"RPMR", intType, "rp_mr"},
		{"State", stringType, "state"}, {"SIPCode", intType, "sip_code"},
		{"RPCause", intType, "rp_cause"}, {"RPCauseText", stringType, "rp_cause_text,omitempty"},
		{"ErrorText", stringType, "error_text"}, {"SentAt", timeType, "sent_at"},
		{"ReportAt", reflect.TypeOf((*time.Time)(nil)), "report_at,omitempty"},
		{"CreatedAt", timeType, "created_at"}, {"UpdatedAt", timeType, "updated_at"},
	})
	assertRecoveredPrefix(t, DeliveryStatus{}, []recoveredField{
		{"MessageID", stringType, "message_id"}, {"IMSI", stringType, "imsi"},
		{"DeviceID", stringType, "device_id"}, {"Peer", stringType, "peer"},
		{"Content", stringType, "content"}, {"PartsTotal", intType, "parts_total"},
		{"Acks", intType, "acks"}, {"State", stringType, "state"},
		{"LastError", stringType, "last_error"}, {"CreatedAt", timeType, "created_at"},
		{"UpdatedAt", timeType, "updated_at"},
		{"Parts", reflect.TypeOf([]DeliveryPartStatus{}), "parts"},
	})
}

func TestRPCauseTextMatchesTS24011(t *testing.T) {
	want := map[int]string{
		0: "normal", 1: "unassigned number", 8: "operator determined barring",
		10: "call barred", 21: "short message transfer rejected",
		29: "facility rejected", 38: "network out of order", 41: "temporary failure",
		69: "requested facility not implemented", 95: "semantically incorrect message",
		111: "protocol error", 255: "unknown",
	}
	for cause, text := range want {
		if got := RPCauseText(cause); got != text {
			t.Errorf("RPCauseText(%d) = %q, want %q", cause, got, text)
		}
	}
}

func TestServiceStatusIsRegistered(t *testing.T) {
	tests := []struct {
		status ServiceStatus
		want   bool
	}{
		{status: ServiceStatus{Registered: true}, want: true},
		{status: ServiceStatus{RegStatus: "  ReGiStErEd "}, want: true},
		{status: ServiceStatus{RegStatus: "Registering"}, want: false},
		{status: ServiceStatus{}, want: false},
	}
	for _, test := range tests {
		if got := test.status.IsRegistered(); got != test.want {
			t.Fatalf("ServiceStatus(%+v).IsRegistered() = %v, want %v", test.status, got, test.want)
		}
	}
}

type recoveredService struct{}

func (recoveredService) CancelUSSD(context.Context, string) error { return nil }
func (recoveredService) ContinueUSSD(context.Context, string, string) (*USSDResult, error) {
	return nil, nil
}
func (recoveredService) GetSMSDeliveryStatus(string) (*DeliveryStatus, error) { return nil, nil }
func (recoveredService) SendSMSWithOptions(context.Context, string, string, SendOptions) (SendOutcome, error) {
	return SendOutcome{}, nil
}
func (recoveredService) SendSMSWithResult(context.Context, string, string) (SendOutcome, error) {
	return SendOutcome{}, nil
}
func (recoveredService) SendUSSD(context.Context, string) (*USSDResult, error) { return nil, nil }
func (recoveredService) Status() map[string]interface{}                        { return nil }
func (recoveredService) StatusSnapshot() ServiceStatus                         { return ServiceStatus{} }
func (recoveredService) Stop(context.Context) error                            { return nil }
func (recoveredService) TriggerRegisterImmediate(string)                       {}

var _ Service = recoveredService{}

func TestRecoveredServiceMethodSet(t *testing.T) {
	serviceType := reflect.TypeOf((*Service)(nil)).Elem()
	want := map[string]reflect.Type{
		"CancelUSSD":               reflect.TypeOf(func(context.Context, string) error { return nil }),
		"ContinueUSSD":             reflect.TypeOf(func(context.Context, string, string) (*USSDResult, error) { return nil, nil }),
		"GetSMSDeliveryStatus":     reflect.TypeOf(func(string) (*DeliveryStatus, error) { return nil, nil }),
		"SendSMSWithOptions":       reflect.TypeOf(func(context.Context, string, string, SendOptions) (SendOutcome, error) { return SendOutcome{}, nil }),
		"SendSMSWithResult":        reflect.TypeOf(func(context.Context, string, string) (SendOutcome, error) { return SendOutcome{}, nil }),
		"SendUSSD":                 reflect.TypeOf(func(context.Context, string) (*USSDResult, error) { return nil, nil }),
		"Status":                   reflect.TypeOf(func() map[string]interface{} { return nil }),
		"StatusSnapshot":           reflect.TypeOf(func() ServiceStatus { return ServiceStatus{} }),
		"Stop":                     reflect.TypeOf(func(context.Context) error { return nil }),
		"TriggerRegisterImmediate": reflect.TypeOf(func(string) {}),
	}
	if serviceType.NumMethod() != len(want) {
		t.Fatalf("Service has %d methods, want %d", serviceType.NumMethod(), len(want))
	}
	for name, wantType := range want {
		method, ok := serviceType.MethodByName(name)
		if !ok || method.Type != wantType {
			t.Fatalf("Service.%s = %v, want %v", name, method.Type, wantType)
		}
	}
}

func TestDeliveryNotFoundTextMatchesRecoveredError(t *testing.T) {
	if got := ErrDeliveryNotFound.Error(); got != "sms delivery not found" {
		t.Fatalf("ErrDeliveryNotFound = %q", got)
	}
	if !errors.Is(ErrDeliveryNotFound, ErrDeliveryNotFound) {
		t.Fatal("delivery sentinel lost errors.Is identity")
	}
}
