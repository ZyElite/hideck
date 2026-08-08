//go:build linux

package driver

import (
	"net"

	"github.com/iniwex5/netlink"
)

type XFRMManager struct {
	undos []func() error
}

func NewXFRMManager() *XFRMManager { return &XFRMManager{} }

type XFRMSAConfig struct {
	Src   net.IP
	Dst   net.IP
	SPI   uint32
	Proto netlink.Proto

	IsAEAD bool

	AeadAlgoName string
	AeadKey      []byte
	AeadICVLen   int

	CryptAlgoName string
	CryptKey      []byte
	AuthAlgoName  string
	AuthKey       []byte
	AuthTruncLen  int

	EncapType     netlink.EncapType
	EncapSrcPort  int
	EncapDstPort  int
	Ifid          int
	Mode          netlink.Mode
	TimeLimitSoft uint64
	TimeLimitHard uint64
	ReplayWindow  int
	SADir         netlink.SADir
	ESN           bool
}

type XFRMSPConfig struct {
	Src *net.IPNet
	Dst *net.IPNet
	Dir netlink.Dir

	TmplSrc   net.IP
	TmplDst   net.IP
	TmplProto netlink.Proto
	TmplMode  netlink.Mode
	TmplSPI   int
	Ifid      int
}
