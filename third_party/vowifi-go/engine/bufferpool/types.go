// Package bufferpool provides fixed-size pools for packet buffers.
package bufferpool

// Lease owns a buffer until Release is called.
//
// The slot pointer is retained separately from bytes so Release can restore
// the full slice header before returning it to sync.Pool.
type Lease struct {
	slot  *[]byte
	bytes []byte
	class int
}

// Bytes returns the requested-length slice owned by the lease.
func (l *Lease) Bytes() []byte {
	if l == nil {
		return nil
	}
	return l.bytes
}
