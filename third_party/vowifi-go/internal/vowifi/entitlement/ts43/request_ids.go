package ts43

import "sync/atomic"

const initialRequestID int64 = 2

var requestSequence atomic.Int64

func init() {
	requestSequence.Store(initialRequestID)
}

// NextRequestIDs reserves one contiguous request-id range for a TS.43 action batch.
func NextRequestIDs(count int) []int {
	if count <= 0 {
		return nil
	}
	last := int(requestSequence.Add(int64(count)))
	first := last - count + 1
	ids := make([]int, count)
	for i := range ids {
		ids[i] = first + i
	}
	return ids
}
