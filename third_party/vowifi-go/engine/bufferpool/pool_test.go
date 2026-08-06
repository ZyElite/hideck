package bufferpool

import "testing"

func TestGetLeaseSize(t *testing.T) {
	cases := []int{0, 1, 100, 512, 513, 1024, 4096, 8192}
	for _, n := range cases {
		l := Get(n)
		if len(l.Bytes()) != n {
			t.Errorf("Get(%d): len = %d", n, len(l.Bytes()))
		}
		if cap(l.buf) < n {
			t.Errorf("Get(%d): cap = %d < n", n, cap(l.buf))
		}
		l.Release()
	}
}

func TestGetReusesPooledBuffer(t *testing.T) {
	l1 := Get(1000)
	buf1 := l1.buf[:cap(l1.buf)]
	l1.Release()
	l2 := Get(1000)
	// The same backing buffer should be reused from the pool.
	if &l2.buf[0] != &buf1[0] {
		t.Error("pooled buffer not reused after Release")
	}
	l2.Release()
}

func TestGetLargeUnpooled(t *testing.T) {
	l := Get(20000)
	if l.class != -1 {
		t.Errorf("large request class = %d, want -1 (unpooled)", l.class)
	}
	if len(l.Bytes()) != 20000 {
		t.Errorf("large request len = %d", len(l.Bytes()))
	}
	l.Release() // no-op for unpooled
}

func TestNilLeaseSafe(t *testing.T) {
	var l *Lease
	if l.Bytes() != nil {
		t.Error("nil lease Bytes should be nil")
	}
	l.Release() // must not panic
}