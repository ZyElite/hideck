//go:build !darwin && !linux

package audiotranscode

import (
	"errors"
	"runtime"
)

func loadNativeLame() (*lameAPI, error) {
	return nil, errors.New("native MP3 encoding is unavailable on " + runtime.GOOS)
}

func loadRecordingAMRDecoder(codec string) (*amrDecoderAPI, error) {
	return nil, errors.New(codec + " recording decoder is unavailable on " + runtime.GOOS)
}
