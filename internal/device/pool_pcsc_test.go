package device

import (
	"context"
	"testing"

	"github.com/iniwex5/vohive/internal/backend"
	"github.com/iniwex5/vohive/internal/config"
	"github.com/iniwex5/vohive/internal/pcsc"
)

type pcscReaderStateBackend struct {
	readers []pcsc.Reader
}

func (state *pcscReaderStateBackend) Readers(context.Context) ([]pcsc.Reader, error) {
	return append([]pcsc.Reader(nil), state.readers...), nil
}

func (*pcscReaderStateBackend) Open(context.Context, pcsc.Selector) (pcsc.Card, error) {
	return nil, pcsc.ErrNoCard
}

type closingPCSCDeviceBackend struct {
	*workerStatusBackendStub
	closed bool
}

func (device *closingPCSCDeviceBackend) Close() error {
	device.closed = true
	return nil
}

func TestReconcilePCSCReadersRemovesWorkerAfterCardRemoval(t *testing.T) {
	cfg := config.DeviceConfig{
		ID: "reader-1", DeviceBackend: backend.BackendPCSC,
		PCSCReaderName: "Reader A", PCSCUSBPath: "1-2",
	}
	pool := NewPool(&config.Config{Devices: []config.DeviceConfig{cfg}})
	t.Cleanup(pool.cancel)
	pool.pcscService = pcsc.NewWithBackend(&pcscReaderStateBackend{readers: []pcsc.Reader{{
		Name: "Reader A", USBPath: "1-2", CardPresent: false,
	}}})
	deviceBackend := &closingPCSCDeviceBackend{workerStatusBackendStub: &workerStatusBackendStub{
		mode: backend.BackendPCSC, simInserted: true,
	}}
	pool.workers[cfg.ID] = &Worker{
		ID: cfg.ID, Config: cfg, Backend: deviceBackend, stop: make(chan struct{}), Pool: pool,
	}

	if err := pool.reconcilePCSCReaders(rescanReconnectOptions{}, []config.DeviceConfig{cfg}, []config.DeviceConfig{cfg}); err != nil {
		t.Fatal(err)
	}
	if worker := pool.GetWorker(cfg.ID); worker != nil {
		t.Fatalf("worker remained after card removal: %+v", worker)
	}
	if !deviceBackend.closed {
		t.Fatal("PC/SC backend was not closed after card removal")
	}
	if len(pool.cfg.Devices) != 1 || pool.cfg.Devices[0].ID != cfg.ID {
		t.Fatalf("configured reader was removed during hot-unplug: %+v", pool.cfg.Devices)
	}
}
