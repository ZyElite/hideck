//go:build !linux

package driver

func NewTUNDevice(string) (*TUNDevice, error) { return nil, errUnsupportedPlatform }

func (t *TUNDevice) Read([]byte) (int, error)  { return 0, errUnsupportedPlatform }
func (t *TUNDevice) Write([]byte) (int, error) { return 0, errUnsupportedPlatform }
func (t *TUNDevice) Close() error              { return errUnsupportedPlatform }
