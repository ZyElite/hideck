package bufferpool

import "sync"

// bufferClasses are the pooled size classes (power-of-two up to 8 KiB, matching
// the decompiled 5-class pool). Requests larger than the biggest class are
// allocated fresh and never returned to a pool.
var bufferClasses = [...]int{512, 1024, 2048, 4096, 8192}

var pools [len(bufferClasses)]sync.Pool

func init() {
	for i := range pools {
		size := bufferClasses[i]
		pools[i].New = func() interface{} { return make([]byte, size) }
	}
}

// classIndex returns the pool class index for n, or -1 if n exceeds the largest
// class.
func classIndex(n int) int {
	if n < 0 {
		n = 0
	}
	for i, size := range bufferClasses {
		if n <= size {
			return i
		}
	}
	return -1
}

// Get returns a Lease over a buffer of at least n bytes. Buffers up to the
// largest class are drawn from a sized sync.Pool; larger buffers are allocated
// fresh.
func Get(n int) *Lease {
	if n < 0 {
		n = 0
	}
	if c := classIndex(n); c >= 0 {
		buf := pools[c].Get().([]byte)
		return &Lease{buf: buf, n: n, class: c}
	}
	return &Lease{buf: make([]byte, n), n: n, class: -1}
}

// Release returns the buffer to its pool. It is safe to call on a nil lease or
// an unpooled lease.
func (l *Lease) Release() {
	if l == nil || l.class < 0 || l.class >= len(pools) {
		return
	}
	// Restore the full class length before returning to the pool.
	buf := l.buf[:cap(l.buf)]
	pools[l.class].Put(buf)
	l.buf = nil
	l.n = 0
	l.class = -1
}
