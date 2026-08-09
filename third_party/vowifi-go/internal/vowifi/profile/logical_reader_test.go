package profile

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

type scriptedLogicalTransport struct {
	responses   map[string][]string
	resolverAID string
	resolverErr error
	openedAID   string
	openErr     error
	openCalls   int
	closeCalls  int
	calls       []string
}

func (stub *scriptedLogicalTransport) ResolveLogicalChannelAID(_, _ string) (string, string, error) {
	return stub.resolverAID, "test", stub.resolverErr
}

func (stub *scriptedLogicalTransport) OpenLogicalChannel(aid string) (int, error) {
	stub.openCalls++
	stub.openedAID = aid
	return 7, stub.openErr
}

func (stub *scriptedLogicalTransport) CloseLogicalChannel(channel int) error {
	if channel != 7 {
		return fmt.Errorf("unexpected channel %d", channel)
	}
	stub.closeCalls++
	return nil
}

func (stub *scriptedLogicalTransport) TransmitAPDU(channel int, command string) (string, error) {
	if channel != 7 {
		return "", fmt.Errorf("unexpected channel %d", channel)
	}
	stub.calls = append(stub.calls, command)
	queue := stub.responses[command]
	if len(queue) == 0 {
		return "", fmt.Errorf("no scripted response for %s", command)
	}
	stub.responses[command] = queue[1:]
	if strings.HasPrefix(queue[0], "ERR:") {
		return "", errors.New(strings.TrimPrefix(queue[0], "ERR:"))
	}
	return queue[0], nil
}

func TestReadISIMIdentityLifecycleAndAPDUFollowUps(t *testing.T) {
	stub := &scriptedLogicalTransport{
		resolverAID: isimAID + "FFFFFFFF8903020000",
		responses: map[string][]string{
			"00A40004026F02": {"6106"},
			"00C0000006":     {"6204800200059000"},
			"00B0000005":     {"6C07"},
			"00B0000007":     {"80056140622E639000"},
			"00A40004026F03": {"62048002000B9000"},
			"00B000000B":     {"8009696D732E746573749000"},
			"00A40004026F04": {"620782050000000C029000"},
			"00B201040C":     {"800A7369703A6140622E639000"},
			"00B202040C":     {"800A7369703A6140622E639000"},
		},
	}

	got, err := ReadISIMIdentity(stub)
	if err != nil {
		t.Fatalf("ReadISIMIdentity() error = %v", err)
	}
	if got.IMPI != "a@b.c" || got.Domain != "ims.test" {
		t.Fatalf("identity = %+v", got)
	}
	if len(got.IMPU) != 1 || got.IMPU[0] != "sip:a@b.c" {
		t.Fatalf("IMPU = %#v", got.IMPU)
	}
	if stub.openedAID != stub.resolverAID || stub.openCalls != 1 || stub.closeCalls != 1 {
		t.Fatalf("lifecycle: aid=%q open=%d close=%d", stub.openedAID, stub.openCalls, stub.closeCalls)
	}
}

func TestReadISIMIdentityFallsBackAIDAndReturnsPartialIdentity(t *testing.T) {
	stub := &scriptedLogicalTransport{
		resolverErr: errors.New("card status unavailable"),
		responses: map[string][]string{
			"00A40004026F02": {"6A829000"},
			"00A40004026F03": {"62048002000B9000"},
			"00B000000B":     {"8009696D732E746573749000"},
			"00A40004026F04": {"6A829000"},
		},
	}
	got, err := ReadISIMIdentity(stub)
	if err != nil {
		t.Fatalf("ReadISIMIdentity() error = %v", err)
	}
	if got.Domain != "ims.test" || got.IMPI != "" || len(got.IMPU) != 0 {
		t.Fatalf("partial identity = %+v", got)
	}
	if stub.openedAID != isimAID || stub.closeCalls != 1 {
		t.Fatalf("fallback lifecycle: aid=%q close=%d", stub.openedAID, stub.closeCalls)
	}
}

func TestReadISIMIdentityErrors(t *testing.T) {
	if _, err := ReadISIMIdentity(nil); err == nil {
		t.Fatal("nil transport accepted")
	}
	stub := &scriptedLogicalTransport{openErr: errors.New("open failed")}
	if _, err := ReadISIMIdentity(stub); err == nil || !strings.Contains(err.Error(), "open failed") {
		t.Fatalf("open error = %v", err)
	}
}

func TestTLVParsingAndIdentitySanitizing(t *testing.T) {
	value := []byte(" user\x00\xff@example.com ")
	data := append([]byte{0x62, 0x81, byte(len(value) + 3), 0x9F, 0x70, byte(len(value))}, value...)
	values := collectTLVValues(data, 0x9F)
	if len(values) != 1 || string(values[0]) != string(value) {
		t.Fatalf("multi-byte TLV values = %X", values)
	}
	nested := append([]byte{0x62, byte(len(value) + 2), 0x80, byte(len(value))}, value...)
	decoded := decodeIdentityValues(nested)
	if len(decoded) != 1 || decoded[0] != "user@example.com" {
		t.Fatalf("decoded identity = %#v", decoded)
	}
	if got := parseTransparentFileSizeFromFCP([]byte{0x62, 0x04, 0x81, 0x02, 0x01, 0x00}); got != 256 {
		t.Fatalf("transparent size = %d", got)
	}
	length, count := parseLinearFixedMetaFromFCP([]byte{0x62, 0x07, 0x82, 0x05, 0x42, 0x21, 0x00, 0x40, 0x03})
	if length != 64 || count != 3 {
		t.Fatalf("linear metadata = %d, %d", length, count)
	}
	length, count = parseLinearFixedMetaFromFCP([]byte{0x62, 0x04, 0x80, 0x02, 0x01, 0x00})
	if length != 0 || count != 256 {
		t.Fatalf("linear fallback metadata = %d, %d", length, count)
	}
}

func TestFollowUpLimitAndWarningStatus(t *testing.T) {
	calls := 0
	transmit := func(command []byte) ([]byte, error) {
		calls++
		return []byte{0x61, 0x01}, nil
	}
	got, err := followUpIfNeeded(transmit, []byte{0, 0, 0, 0, 1}, []byte{0x61, 0x01})
	if err != nil || calls != maxAPDUFollowUps || string(got) != string([]byte{0x61, 0x01}) {
		t.Fatalf("follow-up = %X, calls=%d, err=%v", got, calls, err)
	}
	if data, err := extractSuccessData([]byte{1, 2, 0x62, 0x01}); err != nil || len(data) != 2 {
		t.Fatalf("warning status data=%X err=%v", data, err)
	}
}
