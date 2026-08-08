package driver

import "github.com/songgao/water"

type TUNDevice struct {
	iface *water.Interface
	Name  string
}

func (t *TUNDevice) DeviceName() string {
	return t.Name
}
