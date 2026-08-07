package identity

import (
	"encoding/hex"
	"errors"
	"fmt"
	"testing"
)

const testFullISIMAID = "A0000000871004FFFFFFFF8903020000"

type logicalChannelStub struct {
	files       map[uint16][][]byte
	selected    uint16
	openedAID   string
	openCalls   int
	closeCalls  int
	resolverErr error
}

func (s *logicalChannelStub) ResolveLogicalChannelAID(_, _ string) (string, string, error) {
	if s.resolverErr != nil {
		return "", "test", s.resolverErr
	}
	return testFullISIMAID, "test", nil
}

func (s *logicalChannelStub) OpenLogicalChannel(aid string) (int, error) {
	s.openCalls++
	s.openedAID = aid
	return 3, nil
}

func (s *logicalChannelStub) CloseLogicalChannel(channel int) error {
	if channel != 3 {
		return fmt.Errorf("unexpected channel %d", channel)
	}
	s.closeCalls++
	return nil
}

func (s *logicalChannelStub) TransmitAPDU(channel int, commandHex string) (string, error) {
	if channel != 3 {
		return "", fmt.Errorf("unexpected channel %d", channel)
	}
	command, err := hex.DecodeString(commandHex)
	if err != nil {
		return "", err
	}
	response, err := s.handleCommand(command)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%X9000", response), nil
}

func (s *logicalChannelStub) handleCommand(command []byte) ([]byte, error) {
	if len(command) != 5 && len(command) != 7 {
		return nil, fmt.Errorf("unexpected command %X", command)
	}
	switch command[1] {
	case 0xA4:
		s.selected = uint16(command[5])<<8 | uint16(command[6])
		return s.selectedFileFCP()
	case 0xB0:
		return append([]byte(nil), s.files[s.selected][0]...), nil
	case 0xB2:
		record := int(command[2]) - 1
		if record < 0 || record >= len(s.files[s.selected]) {
			return nil, fmt.Errorf("unexpected record %d", record+1)
		}
		return append([]byte(nil), s.files[s.selected][record]...), nil
	default:
		return nil, fmt.Errorf("unexpected command %X", command)
	}
}

func (s *logicalChannelStub) selectedFileFCP() ([]byte, error) {
	records := s.files[s.selected]
	if len(records) == 0 {
		return nil, fmt.Errorf("unknown file %04X", s.selected)
	}
	recordLength := len(records[0])
	if s.selected == efIMPU {
		return []byte{0x62, 0x07, 0x82, 0x05, 0x00, 0x00, byte(recordLength >> 8), byte(recordLength), byte(len(records))}, nil
	}
	return []byte{0x62, 0x04, 0x80, 0x02, byte(recordLength >> 8), byte(recordLength)}, nil
}

func TestReadISIMIdentityFromLogicalChannel(t *testing.T) {
	stub := &logicalChannelStub{files: map[uint16][][]byte{
		efIMPI:   {isimDataObject("234102356143376@ims.mnc010.mcc234.3gppnetwork.org")},
		efDomain: {isimDataObject("ims.mnc010.mcc234.3gppnetwork.org")},
		efIMPU:   {isimDataObject("sip:234102356143376@ims.mnc010.mcc234.3gppnetwork.org")},
	}}

	got, err := ReadISIMIdentityFromLogicalChannel(stub)
	if err != nil {
		t.Fatalf("ReadISIMIdentityFromLogicalChannel() error = %v", err)
	}
	if got.IMPI != "234102356143376@ims.mnc010.mcc234.3gppnetwork.org" {
		t.Fatalf("IMPI = %q", got.IMPI)
	}
	if got.Domain != "ims.mnc010.mcc234.3gppnetwork.org" {
		t.Fatalf("Domain = %q", got.Domain)
	}
	if len(got.IMPU) != 1 || got.IMPU[0] != "sip:234102356143376@ims.mnc010.mcc234.3gppnetwork.org" {
		t.Fatalf("IMPU = %#v", got.IMPU)
	}
	if stub.openedAID != testFullISIMAID || stub.openCalls != 1 || stub.closeCalls != 1 {
		t.Fatalf("logical channel lifecycle: aid=%q open=%d close=%d", stub.openedAID, stub.openCalls, stub.closeCalls)
	}
}

func TestReadISIMIdentityFromLogicalChannelStopsOnResolverError(t *testing.T) {
	stub := &logicalChannelStub{resolverErr: errors.New("card status unavailable")}

	_, err := ReadISIMIdentityFromLogicalChannel(stub)
	if err == nil || err.Error() != "identity: resolve ISIM AID: card status unavailable" {
		t.Fatalf("error = %v", err)
	}
	if stub.openCalls != 0 || stub.closeCalls != 0 {
		t.Fatalf("logical channel used after resolver error: open=%d close=%d", stub.openCalls, stub.closeCalls)
	}
}

func isimDataObject(value string) []byte {
	return append([]byte{0x80, byte(len(value))}, []byte(value)...)
}
