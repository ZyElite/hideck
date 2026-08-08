package bufferpool

import (
	"runtime"
	"sync"
	"testing"
	"unsafe"
)

func TestGetLeaseSize(t *testing.T) {
	cases := []struct {
		requested int
		capacity  int
	}{
		{-1, 512}, {0, 512}, {1, 512}, {512, 512}, {513, 1024},
		{1024, 1024}, {4096, 4096}, {8192, 8192}, {8193, 8193},
	}
	for _, test := range cases {
		lease := Get(test.requested)
		wantLength := max(test.requested, 0)
		if len(lease.Bytes()) != wantLength {
			t.Errorf("Get(%d): len = %d, want %d", test.requested, len(lease.Bytes()), wantLength)
		}
		if cap(lease.Bytes()) != test.capacity {
			t.Errorf("Get(%d): cap = %d, want %d", test.requested, cap(lease.Bytes()), test.capacity)
		}
		lease.Release()
	}
}

func TestGetReusesEveryPooledClass(t *testing.T) {
	previousProcs := runtime.GOMAXPROCS(1)
	defer runtime.GOMAXPROCS(previousProcs)

	for _, classSize := range bufferClasses {
		allocations := testing.AllocsPerRun(100, func() {
			lease := Get(classSize)
			lease.Release()
		})
		if allocations != 0 {
			t.Errorf("%d-byte Get/Release allocations = %v, want 0", classSize, allocations)
		}
	}
}

func TestGetLargeUnpooled(t *testing.T) {
	lease := Get(20000)
	if lease.class != -1 {
		t.Errorf("large request class = %d, want -1", lease.class)
	}
	if lease.slot != nil {
		t.Error("large request unexpectedly retains a pool slot")
	}
	lease.Release()
	assertReleased(t, lease)
}

func TestNilLeaseSafe(t *testing.T) {
	var lease *Lease
	if lease.Bytes() != nil {
		t.Error("nil lease Bytes should be nil")
	}
	lease.Release()
}

func TestReleaseClearsLeaseAndIsIdempotent(t *testing.T) {
	lease := Get(100)
	lease.Release()
	assertReleased(t, lease)
	lease.Release()
	assertReleased(t, lease)
}

func TestLeaseMatchesLegacyFiveWordLayout(t *testing.T) {
	want := 5 * unsafe.Sizeof(uintptr(0))
	if got := unsafe.Sizeof(Lease{}); got != want {
		t.Fatalf("Lease size = %d, want %d", got, want)
	}
}

func TestConcurrentGetRelease(t *testing.T) {
	const goroutines = 32
	const iterations = 500
	var group sync.WaitGroup
	group.Add(goroutines)
	for worker := 0; worker < goroutines; worker++ {
		go func(value byte) {
			defer group.Done()
			for range iterations {
				lease := Get(1500)
				lease.Bytes()[0] = value
				lease.Release()
			}
		}(byte(worker))
	}
	group.Wait()
}

func assertReleased(t *testing.T, lease Lease) {
	t.Helper()
	if lease.slot != nil || lease.Bytes() != nil || lease.class != -1 {
		t.Fatalf("lease not cleared: slot=%p bytes=%v class=%d", lease.slot, lease.Bytes(), lease.class)
	}
}
