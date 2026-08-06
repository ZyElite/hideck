// Package bufferpool provides fixed-size buffer pools for IKE/ESP packet
// buffers, recovered from the decompiled engine/bufferpool.
package bufferpool

// Lease is a buffer obtained from the pool. Release returns it.
type Lease struct {
	buf   []byte // backing buffer (cap = class size)
	n     int    // requested length
	class int    // pool class index, -1 if unpooled
}

// Bytes returns the leased slice (buf[:n]).
func (l *Lease) Bytes() []byte {
	if l == nil {
		return nil
	}
	return l.buf[:l.n]
}