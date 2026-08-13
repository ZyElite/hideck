//go:build !darwin && !linux

package audiotranscode

import (
	"errors"
	"runtime"
)

func loadAMRNBRealtimeAPI() (*amrRealtimeAPI, error) {
	return nil, errors.New("AMR realtime codec is unavailable on " + runtime.GOOS)
}

func loadAMRWBRealtimeAPI() (*amrRealtimeAPI, error) {
	return nil, errors.New("AMR-WB realtime codec is unavailable on " + runtime.GOOS)
}
