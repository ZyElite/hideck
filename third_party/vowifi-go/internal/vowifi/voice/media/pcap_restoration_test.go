package media

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestLegacyPCAPPathAndFileLifecycle(t *testing.T) {
	fixed := time.Date(2026, time.August, 10, 14, 5, 9, 123, time.Local)
	if got := legacyCapturePath("", "device-31", fixed); got != "pcap/rtp_device-31_20260810_140509.pcap" {
		t.Fatalf("default legacy capture path = %q", got)
	}

	relay := NewRTPRelay(nil, nil)
	relay.SetLogContext("device-31", "trace-31")
	directory := t.TempDir()
	if err := relay.StartPCAP(directory); err != nil {
		t.Fatal(err)
	}
	if err := relay.StartPCAP(directory); err == nil || err.Error() != "PCAP 录制已在进行中" {
		t.Fatalf("duplicate legacy StartPCAP error = %v", err)
	}

	relay.pcapMu.Lock()
	file := relay.pcapFile
	relay.pcapMu.Unlock()
	if file == nil {
		t.Fatal("legacy StartPCAP did not retain its file")
	}
	namePattern := regexp.MustCompile(`^rtp_device-31_[0-9]{8}_[0-9]{6}\.pcap$`)
	if !namePattern.MatchString(filepath.Base(file.Name())) {
		t.Fatalf("legacy capture filename = %q", file.Name())
	}
	header := make([]byte, len(pcapGlobalHeader()))
	if _, err := file.ReadAt(header, 0); err != nil {
		t.Fatalf("legacy PCAP file is not open for reading: %v", err)
	}
	if !bytes.Equal(header, pcapGlobalHeader()) {
		t.Fatalf("legacy global header = %x", header)
	}
	relay.writePCAPPacket([]byte{1, 2, 3}, pcapDirectionLANToIMS)
	relay.StopPCAP()
	if _, err := os.Stat(file.Name()); err != nil {
		t.Fatal(err)
	}
}

func TestLegacyPCAPDoesNotCreateOutputDirectory(t *testing.T) {
	relay := NewRTPRelay(nil, nil)
	relay.SetLogContext("device-31")
	missing := filepath.Join(t.TempDir(), "missing")
	err := relay.StartPCAP(missing)
	if err == nil || !strings.Contains(err.Error(), "创建 PCAP 文件失败") {
		t.Fatalf("legacy missing-directory error = %v", err)
	}
	if _, statErr := os.Stat(missing); !os.IsNotExist(statErr) {
		t.Fatalf("legacy StartPCAP created output directory: %v", statErr)
	}
}

func TestCurrentPCAPTargetAndErrorsRemainExplicit(t *testing.T) {
	relay := NewRTPRelay(nil, nil)
	relay.SetLogContext("device/31")
	path := filepath.Join(t.TempDir(), "nested", "capture.pcap")
	if err := relay.StartPCAPCurrent(path); err != nil {
		t.Fatal(err)
	}
	relay.writePCAPPacket([]byte{4, 5, 6}, pcapDirectionIMSToLAN)
	if err := relay.StopPCAPCurrent(); err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(path); err != nil || len(data) != 24+16+1+3 {
		t.Fatalf("current direct capture bytes=%d err=%v", len(data), err)
	}

	var nilWriter *synchronizedCaptureWriter
	if err := relay.StartPCAPCurrent(nilWriter); err == nil || !strings.Contains(err.Error(), "writer is nil") {
		t.Fatalf("typed nil writer error = %v", err)
	}
	closeFailure := errors.New("forced close failure")
	writer := &synchronizedCaptureWriter{closeErr: closeFailure}
	if err := relay.StartPCAPCurrent(writer); err != nil {
		t.Fatal(err)
	}
	if err := relay.StopPCAPCurrent(); !errors.Is(err, closeFailure) {
		t.Fatalf("close failure = %v", err)
	}
}

func TestCurrentPCAPHeaderFailureClosesWriter(t *testing.T) {
	writeFailure := errors.New("forced header failure")
	closeFailure := errors.New("forced cleanup failure")
	writer := &headerFailCaptureWriter{writeErr: writeFailure, closeErr: closeFailure}
	relay := NewRTPRelay(nil, nil)
	err := relay.StartPCAPCurrent(writer)
	if !errors.Is(err, writeFailure) || !errors.Is(err, closeFailure) {
		t.Fatalf("header failure = %v", err)
	}
	if !writer.closed {
		t.Fatal("failed header writer was not closed")
	}
	if relay.pcapEnable || relay.pcapWriter != nil || relay.pcapFile != nil {
		t.Fatal("failed header start retained active capture state")
	}
}

func TestPCAPConcurrentWriteAndStop(t *testing.T) {
	for iteration := 0; iteration < 20; iteration++ {
		relay := NewRTPRelay(nil, nil)
		writer := &synchronizedCaptureWriter{}
		if err := relay.StartPCAPCurrent(writer); err != nil {
			t.Fatal(err)
		}
		var workers sync.WaitGroup
		for worker := 0; worker < 4; worker++ {
			workers.Add(1)
			go func() {
				defer workers.Done()
				for packet := 0; packet < 25; packet++ {
					relay.writePCAPPacket([]byte{1, 2, 3, 4}, pcapDirectionLANToIMS)
				}
			}()
		}
		if err := relay.StopPCAPCurrent(); err != nil {
			t.Fatal(err)
		}
		workers.Wait()
		if writer.wroteAfterClose {
			t.Fatal("capture write overtook serialized close")
		}
	}
}

func TestWriteAllRejectsInvalidWriterCount(t *testing.T) {
	if err := writeAll(invalidCountWriter{}, []byte{1}); !errors.Is(err, io.ErrShortWrite) {
		t.Fatalf("invalid writer count error = %v", err)
	}
}

type synchronizedCaptureWriter struct {
	mu              sync.Mutex
	data            bytes.Buffer
	closed          bool
	wroteAfterClose bool
	closeErr        error
}

func (w *synchronizedCaptureWriter) Write(data []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		w.wroteAfterClose = true
		return 0, errors.New("write after close")
	}
	return w.data.Write(data)
}

func (w *synchronizedCaptureWriter) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.closed = true
	return w.closeErr
}

type headerFailCaptureWriter struct {
	writeErr error
	closeErr error
	closed   bool
}

func (w *headerFailCaptureWriter) Write([]byte) (int, error) { return 0, w.writeErr }

func (w *headerFailCaptureWriter) Close() error {
	w.closed = true
	return w.closeErr
}

type invalidCountWriter struct{}

func (invalidCountWriter) Write(data []byte) (int, error) { return len(data) + 1, nil }
