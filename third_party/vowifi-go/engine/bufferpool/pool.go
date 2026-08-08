package bufferpool

import "sync"

var bufferClasses = [...]int{512, 1024, 2048, 4096, 8192}

var pools [len(bufferClasses)]sync.Pool

func init() {
	for i := range pools {
		size := bufferClasses[i]
		pools[i].New = func() any {
			buffer := make([]byte, size)
			return &buffer
		}
	}
}

func classIndex(n int) int {
	for i, size := range bufferClasses {
		if n <= size {
			return i
		}
	}
	return -1
}

// Get returns a lease whose visible length is n. Negative sizes are treated as
// zero, matching make([]byte, 0) rather than panicking.
func Get(n int) Lease {
	if n < 0 {
		n = 0
	}
	class := classIndex(n)
	if class < 0 {
		return Lease{bytes: make([]byte, n), class: -1}
	}
	return pooledLease(n, class)
}

func pooledLease(n, class int) Lease {
	slot, ok := pools[class].Get().(*[]byte)
	classSize := bufferClasses[class]
	if !ok || slot == nil || cap(*slot) < classSize {
		buffer := make([]byte, classSize)
		slot = &buffer
	}
	return Lease{slot: slot, bytes: (*slot)[:n], class: class}
}

// Release returns a pooled buffer and clears the lease metadata. Unpooled
// leases are cleared without being retained.
func (l *Lease) Release() {
	if l == nil || l.bytes == nil {
		return
	}
	if l.class >= 0 && l.class < len(pools) {
		classSize := bufferClasses[l.class]
		if l.slot != nil && cap(l.bytes) >= classSize {
			*l.slot = l.bytes[:classSize:cap(l.bytes)]
			pools[l.class].Put(l.slot)
		}
	}
	*l = Lease{class: -1}
}
