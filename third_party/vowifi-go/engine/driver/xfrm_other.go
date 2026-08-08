//go:build !linux

package driver

import (
	"net"
	"time"

	"go.uber.org/multierr"
)

type XFRMManager struct {
	undos []func() error
}

func NewXFRMManager() *XFRMManager { return &XFRMManager{} }

type XFRMSAConfig struct {
	Src           net.IP
	Dst           net.IP
	SPI           uint32
	Proto         uint8
	IsAEAD        bool
	AeadAlgoName  string
	AeadKey       []byte
	AeadICVLen    int
	CryptAlgoName string
	CryptKey      []byte
	AuthAlgoName  string
	AuthKey       []byte
	AuthTruncLen  int
	EncapType     uint8
	EncapSrcPort  int
	EncapDstPort  int
	Ifid          int
	Mode          uint8
	TimeLimitSoft uint64
	TimeLimitHard uint64
	ReplayWindow  int
	SADir         uint8
	ESN           bool
}

type XFRMSPConfig struct {
	Src       *net.IPNet
	Dst       *net.IPNet
	Dir       uint8
	TmplSrc   net.IP
	TmplDst   net.IP
	TmplProto uint8
	TmplMode  uint8
	TmplSPI   int
	Ifid      int
}

func (x *XFRMManager) FlushAll()              { panic(errUnsupportedPlatform) }
func (x *XFRMManager) FlushAllChecked() error { return errUnsupportedPlatform }

func (x *XFRMManager) AddXFRMInterface(string, uint32, ...int) error {
	return errUnsupportedPlatform
}

func (x *XFRMManager) DelXFRMInterface(string) error { return errUnsupportedPlatform }
func (x *XFRMManager) AddSA(any) error               { return errUnsupportedPlatform }
func (x *XFRMManager) UpdateSA(any) error            { return errUnsupportedPlatform }
func (x *XFRMManager) DelSA(...any) error            { return errUnsupportedPlatform }
func (x *XFRMManager) AddSP(any) error               { return errUnsupportedPlatform }
func (x *XFRMManager) UpdateSP(any) error            { return errUnsupportedPlatform }
func (x *XFRMManager) DelSP(any) error               { return errUnsupportedPlatform }
func (x *XFRMManager) FlushByIP(net.IP)              { panic(errUnsupportedPlatform) }
func (x *XFRMManager) FlushByIPChecked(net.IP) error { return errUnsupportedPlatform }

func (x *XFRMManager) GetSALastUsed(uint32, net.IP, net.IP, uint8) (uint64, error) {
	return 0, errUnsupportedPlatform
}

func (x *XFRMManager) GetStateLastUsed(any) (time.Time, error) {
	return time.Time{}, errUnsupportedPlatform
}

func (x *XFRMManager) Cleanup()              { _ = x.cleanup() }
func (x *XFRMManager) CleanupChecked() error { return x.cleanup() }

func (x *XFRMManager) cleanup() error {
	var result error
	for index := len(x.undos) - 1; index >= 0; index-- {
		result = multierr.Append(result, x.undos[index]())
	}
	x.undos = nil
	return result
}

func (x *XFRMManager) UndoFuncs() []func() error { return x.undos }
