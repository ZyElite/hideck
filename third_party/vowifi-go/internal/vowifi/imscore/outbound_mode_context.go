package imscore

import (
	"net"

	"github.com/emiago/sipgo"
)

// outboundModeContext is the immutable signaling-route snapshot captured for
// one outbound request in v1.5.5.
type outboundModeContext struct {
	Kind                    string
	Mode                    string
	IPSec3GPP               bool
	Config                  IMSConfig
	Generation              uint64
	SignalingReady          bool
	SignalingNotReadyReason string
	LocalIP                 string
	LocalHost               string
	LocalPortC              int
	LocalPortS              int
	RemoteIP                string
	RemotePortS             int
	Registrar               string
	ServiceRoute            string
	RouteHeader             string
	SecVerify               string
	ContactID               string
	PANI                    string
	AOR                     string
	Transport               string
	TCPConn                 net.Conn
	UDPConn                 net.PacketConn
	Client                  *sipgo.Client
	SkipGenericRawLog       bool
}
