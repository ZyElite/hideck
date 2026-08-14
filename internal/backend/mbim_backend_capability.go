package backend

import "github.com/yibaiba/hideck/pkg/mbim"

func (b *MBIMBackend) Capability() *mbim.Capabilities {
	return b.source.Capability()
}
