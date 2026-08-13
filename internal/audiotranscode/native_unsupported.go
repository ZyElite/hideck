//go:build !darwin && !linux

package audiotranscode

import (
	"errors"
	"runtime"
)

func loadNativeLibraries() (nativeLibraries, error) {
	return nativeLibraries{}, errors.New("native audio transcoding is unavailable on " + runtime.GOOS)
}
