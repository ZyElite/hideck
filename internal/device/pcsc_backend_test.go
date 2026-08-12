package device

import (
	"context"
	"errors"
	"testing"

	"github.com/iniwex5/vohive/internal/backend"
	"github.com/iniwex5/vohive/internal/pcsc"
	"github.com/iniwex5/vowifi-go/runtimehost/identity"
)

type contextCheckingPCSCCard struct {
	responses []struct {
		data []byte
		sw   uint16
	}
}

func (card *contextCheckingPCSCCard) Transmit(ctx context.Context, _ []byte) ([]byte, uint16, error) {
	if err := ctx.Err(); err != nil {
		return nil, 0, err
	}
	response := card.responses[0]
	card.responses = card.responses[1:]
	return response.data, response.sw, nil
}

func (*contextCheckingPCSCCard) Close() error { return nil }

type singlePCSCCardBackend struct{ card pcsc.Card }

func (*singlePCSCCardBackend) Readers(context.Context) ([]pcsc.Reader, error) {
	return []pcsc.Reader{{Name: "Reader A", USBPath: "1-2", CardPresent: true}}, nil
}

func (backend *singlePCSCCardBackend) Open(context.Context, pcsc.Selector) (pcsc.Card, error) {
	return backend.card, nil
}

func TestPCSCMetadataUsesExplicitMNCLength(t *testing.T) {
	metadata := simMetadataFromPCSCIdentity(pcsc.Identity{IMSI: "310260123456789", MNCLength: 3})
	if metadata == nil || metadata.NativeMCC != "310" || metadata.NativeMNC != "260" {
		t.Fatalf("metadata = %#v, want 310/260", metadata)
	}
}

func TestPCSCMetadataUsesOnlyKnownHPLMNAssignmentsWithoutEFAD(t *testing.T) {
	tests := map[string]string{
		"204040123456789": "204/04",
		"234150123456789": "234/15",
		"234870123456789": "234/87",
	}
	for imsi, want := range tests {
		metadata := simMetadataFromPCSCIdentity(pcsc.Identity{IMSI: imsi})
		if metadata == nil || metadata.NativeMCC+"/"+metadata.NativeMNC != want {
			t.Fatalf("metadata for %s = %#v, want %s", imsi, metadata, want)
		}
	}
	if metadata := simMetadataFromPCSCIdentity(pcsc.Identity{IMSI: "999990123456789"}); metadata != nil {
		t.Fatalf("unknown HPLMN metadata = %#v, want nil", metadata)
	}
}

func TestPCSCModemAdapterMapsMissingISIMApplication(t *testing.T) {
	backendStub := &workerStatusBackendStub{
		mode: backend.BackendPCSC, openLogicalChannelErr: pcsc.ErrApplicationNotFound,
	}
	adapter := newPCSCModemAdapter("reader-1", backendStub)

	_, err := adapter.GetISIMIdentity()
	if !errors.Is(err, identity.ErrISIMUnavailable) {
		t.Fatalf("GetISIMIdentity() error = %v, want ErrISIMUnavailable", err)
	}
	if backendStub.openLogicalChannelCalls != 1 {
		t.Fatalf("open logical channel calls = %d, want 1", backendStub.openLogicalChannelCalls)
	}
}

func TestPCSCModemAdapterReportsReaderCapabilities(t *testing.T) {
	capabilities := newPCSCModemAdapter("reader-1", &workerStatusBackendStub{}).Capabilities()
	if !capabilities.Reader || !capabilities.SIM || !capabilities.HasUSIM || capabilities.Modem {
		t.Fatalf("PC/SC capabilities = %+v", capabilities)
	}
}

func TestPCSCLogicalChannelUsesCurrentOperationContext(t *testing.T) {
	card := &contextCheckingPCSCCard{responses: []struct {
		data []byte
		sw   uint16
	}{
		{data: []byte{1}, sw: 0x9000},
		{sw: 0x9000},
		{data: []byte{0xAA}, sw: 0x9000},
		{sw: 0x9000},
	}}
	service := pcsc.NewWithBackend(&singlePCSCCardBackend{card: card})
	deviceBackend, err := newPCSCDeviceBackend(service, pcsc.Selector{USBPath: "1-2"}, "")
	if err != nil {
		t.Fatal(err)
	}
	openCtx, cancelOpen := context.WithCancel(context.Background())
	logical, err := deviceBackend.OpenLogicalChannel(openCtx, "A0000000871004")
	if err != nil {
		t.Fatal(err)
	}
	cancelOpen()

	response, err := deviceBackend.TransmitAPDU(context.Background(), logical, "00A40000")
	if err != nil {
		t.Fatalf("TransmitAPDU() inherited canceled open context: %v", err)
	}
	if response != "AA9000" {
		t.Fatalf("TransmitAPDU() = %q, want AA9000", response)
	}
	if err := deviceBackend.CloseLogicalChannel(context.Background(), logical); err != nil {
		t.Fatal(err)
	}
}
