//go:build !linux

package device

// UdevWatcher is a no-op on non-Linux platforms (udev is Linux-only).
type UdevWatcher struct{}

// NewUdevWatcher returns a no-op watcher.
func NewUdevWatcher(pool *Pool) *UdevWatcher { return &UdevWatcher{} }

// Start is a no-op.
func (w *UdevWatcher) Start() {}

// Stop is a no-op.
func (w *UdevWatcher) Stop() {}
