package swu

import (
	"fmt"
	"os"
	"sync"
	"time"
)

// WiresharkDebugger logs IKE/ESP traffic in a pcap-like form for diagnostics.
// It writes human-readable lines to a file when configured.
type WiresharkDebugger struct {
	mu   sync.Mutex
	file *os.File
}

// NewWiresharkDebugger creates a debugger that writes to the given path.
func NewWiresharkDebugger() *WiresharkDebugger {
	return &WiresharkDebugger{}
}

// SetOutput attaches a log file to the debugger.
func (w *WiresharkDebugger) SetOutput(path string) error {
	if path == "" {
		return nil
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	w.mu.Lock()
	w.file = f
	w.mu.Unlock()
	return nil
}

// Close closes the log file.
func (w *WiresharkDebugger) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.file != nil {
		err := w.file.Close()
		w.file = nil
		return err
	}
	return nil
}

// writeLine writes a timestamped line to the log.
func (w *WiresharkDebugger) writeLine(line string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.file == nil {
		return
	}
	fmt.Fprintf(w.file, "%s %s\n", time.Now().Format("15:04:05.000"), line)
}

// LogRaw logs a raw diagnostic line.
func (w *WiresharkDebugger) LogRaw(line string) {
	w.writeLine(line)
}

// LogIKESAKeys logs the IKE SA key material summary.
func (w *WiresharkDebugger) LogIKESAKeys() {
	w.writeLine("IKE SA keys derived")
}

// LogESPKeys logs the ESP key material summary.
func (w *WiresharkDebugger) LogESPKeys() {
	w.writeLine("ESP keys derived")
}

// LogChildSA logs a CHILD_SA event.
func (w *WiresharkDebugger) LogChildSA() {
	w.writeLine("CHILD_SA established")
}
