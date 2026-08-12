//go:build !darwin && !linux

package pcsc

import "context"

type unsupportedBackend struct{}

func newSystemBackend() Backend { return unsupportedBackend{} }

func (unsupportedBackend) Readers(context.Context) ([]Reader, error) { return nil, ErrUnsupported }

func (unsupportedBackend) Open(context.Context, Selector) (Card, error) { return nil, ErrUnsupported }
