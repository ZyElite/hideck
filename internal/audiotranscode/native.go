//go:build darwin || linux

package audiotranscode

import (
	"errors"
	"fmt"
	"runtime"

	"github.com/ebitengine/purego"
)

func loadNativeLibraries() (nativeLibraries, error) {
	lame, err := loadLameAPI()
	if err != nil {
		return nativeLibraries{}, err
	}
	amrNB, err := loadAMRDecoderAPI(amrDecoderSymbols{
		libraries: []string{"libopencore-amrnb.so.0", "libopencore-amrnb.so", "libopencore-amrnb.dylib"},
		init:      "Decoder_Interface_init", decode: "Decoder_Interface_Decode", close: "Decoder_Interface_exit",
	})
	if err != nil {
		return nativeLibraries{}, err
	}
	amrWB, err := loadAMRDecoderAPI(amrDecoderSymbols{
		libraries: []string{"libopencore-amrwb.so.0", "libopencore-amrwb.so", "libopencore-amrwb.dylib"},
		init:      "D_IF_init", decode: "D_IF_decode", close: "D_IF_exit",
	})
	if err != nil {
		return nativeLibraries{}, err
	}
	return nativeLibraries{lame: lame, amrNB: amrNB, amrWB: amrWB}, nil
}

func openLibrary(candidates []string) (uintptr, error) {
	var failures []error
	for _, candidate := range candidates {
		handle, err := purego.Dlopen(candidate, purego.RTLD_NOW|purego.RTLD_LOCAL)
		if err == nil {
			return handle, nil
		}
		failures = append(failures, err)
	}
	return 0, errors.Join(failures...)
}

func registerNativeSymbol(handle uintptr, target any, name string) (err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("resolve native audio symbol %s: %v", name, recovered)
		}
	}()
	purego.RegisterLibFunc(target, handle, name)
	return nil
}

type amrDecoderSymbols struct {
	libraries []string
	init      string
	decode    string
	close     string
}

func loadAMRDecoderAPI(symbols amrDecoderSymbols) (*amrDecoderAPI, error) {
	handle, err := openLibrary(symbols.libraries)
	if err != nil {
		return nil, fmt.Errorf("load AMR decoder library on %s: %w", runtime.GOOS, err)
	}
	api := &amrDecoderAPI{}
	bindings := []struct {
		target any
		name   string
	}{{&api.init, symbols.init}, {&api.decode, symbols.decode}, {&api.close, symbols.close}}
	for _, binding := range bindings {
		if err := registerNativeSymbol(handle, binding.target, binding.name); err != nil {
			_ = purego.Dlclose(handle)
			return nil, err
		}
	}
	return api, nil
}

func loadLameAPI() (*lameAPI, error) {
	handle, err := openLibrary([]string{"libmp3lame.so.0", "libmp3lame.so", "libmp3lame.dylib"})
	if err != nil {
		return nil, fmt.Errorf("load MP3 encoder library on %s: %w", runtime.GOOS, err)
	}
	api := &lameAPI{}
	bindings := []struct {
		target any
		name   string
	}{
		{&api.init, "lame_init"}, {&api.close, "lame_close"},
		{&api.setSampleRate, "lame_set_in_samplerate"}, {&api.setChannels, "lame_set_num_channels"},
		{&api.setBitrate, "lame_set_brate"}, {&api.setMode, "lame_set_mode"},
		{&api.setQuality, "lame_set_quality"}, {&api.initParams, "lame_init_params"},
		{&api.encodeBuffer, "lame_encode_buffer"}, {&api.encodeFlush, "lame_encode_flush"},
	}
	for _, binding := range bindings {
		if err := registerNativeSymbol(handle, binding.target, binding.name); err != nil {
			_ = purego.Dlclose(handle)
			return nil, err
		}
	}
	return api, nil
}
