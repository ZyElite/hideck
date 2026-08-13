//go:build darwin || linux

package audiotranscode

import (
	"fmt"
	"runtime"
)

func loadAMRNBRealtimeAPI() (*amrRealtimeAPI, error) {
	handle, err := openLibrary([]string{
		"libopencore-amrnb.so.0", "libopencore-amrnb.so", "libopencore-amrnb.dylib",
	})
	if err != nil {
		return nil, fmt.Errorf("load AMR decoder/encoder library on %s: %w", runtime.GOOS, err)
	}
	decoder, err := bindAMRDecoder(handle, "Decoder_Interface_init", "Decoder_Interface_Decode", "Decoder_Interface_exit")
	if err != nil {
		closeLibrary(handle)
		return nil, err
	}
	var init func(int) uintptr
	var encode func(uintptr, int, []int16, []byte, int) int
	var closeEncoder func(uintptr)
	if err := bindNativeFunctions(handle, []nativeBinding{
		{&init, "Encoder_Interface_init"}, {&encode, "Encoder_Interface_Encode"}, {&closeEncoder, "Encoder_Interface_exit"},
	}); err != nil {
		closeLibrary(handle)
		return nil, fmt.Errorf("AMR encoder is unavailable: %w", err)
	}
	return &amrRealtimeAPI{decoder: decoder, encoder: &amrEncoderAPI{
		init: func() uintptr { return init(0) },
		encode: func(state uintptr, mode int, pcm []int16, output []byte) int {
			return encode(state, mode, pcm, output, 0)
		},
		close: closeEncoder,
	}}, nil
}

func loadAMRWBRealtimeAPI() (*amrRealtimeAPI, error) {
	decoderHandle, err := openLibrary([]string{
		"libopencore-amrwb.so.0", "libopencore-amrwb.so", "libopencore-amrwb.dylib",
	})
	if err != nil {
		return nil, fmt.Errorf("load AMR-WB decoder library on %s: %w", runtime.GOOS, err)
	}
	decoder, err := bindAMRDecoder(decoderHandle, "D_IF_init", "D_IF_decode", "D_IF_exit")
	if err != nil {
		closeLibrary(decoderHandle)
		return nil, err
	}
	encoderHandle, err := openLibrary([]string{
		"libvo-amrwbenc.so.0", "libvo-amrwbenc.so", "libvo-amrwbenc.dylib",
	})
	if err != nil {
		closeLibrary(decoderHandle)
		return nil, fmt.Errorf("load AMR-WB encoder library on %s: %w", runtime.GOOS, err)
	}
	var init func() uintptr
	var encode func(uintptr, int, []int16, []byte, int) int
	var closeEncoder func(uintptr)
	if err := bindNativeFunctions(encoderHandle, []nativeBinding{
		{&init, "E_IF_init"}, {&encode, "E_IF_encode"}, {&closeEncoder, "E_IF_exit"},
	}); err != nil {
		closeLibrary(decoderHandle)
		closeLibrary(encoderHandle)
		return nil, fmt.Errorf("AMR-WB encoder is unavailable: %w", err)
	}
	return &amrRealtimeAPI{decoder: decoder, encoder: &amrEncoderAPI{
		init: init,
		encode: func(state uintptr, mode int, pcm []int16, output []byte) int {
			return encode(state, mode, pcm, output, 0)
		},
		close: closeEncoder,
	}}, nil
}

type nativeBinding struct {
	target any
	name   string
}

func bindAMRDecoder(handle uintptr, initName, decodeName, closeName string) (*amrDecoderAPI, error) {
	api := &amrDecoderAPI{}
	err := bindNativeFunctions(handle, []nativeBinding{
		{&api.init, initName}, {&api.decode, decodeName}, {&api.close, closeName},
	})
	if err != nil {
		return nil, err
	}
	return api, nil
}

func bindNativeFunctions(handle uintptr, bindings []nativeBinding) error {
	for _, binding := range bindings {
		if err := registerNativeSymbol(handle, binding.target, binding.name); err != nil {
			return err
		}
	}
	return nil
}
