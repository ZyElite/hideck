package ipsec

import "fmt"

const (
	linuxErrMessageSize        = 90
	linuxErrNetworkUnreachable = 101
	linuxErrHostUnreachable    = 113
	minimumReportedPMTU        = 501
	maximumReportedPMTU        = 1499
)

func netEventFromExtendedError(errno, info uint32) (NetEvent, bool) {
	switch errno {
	case linuxErrMessageSize:
		if info < minimumReportedPMTU || info > maximumReportedPMTU {
			return NetEvent{}, false
		}
		return NetEvent{
			Type: EventPathMTU, PMTU: info, Reason: fmt.Sprintf("MTU changed to %d", info),
		}, true
	case linuxErrNetworkUnreachable, linuxErrHostUnreachable:
		return NetEvent{
			Type: EventNetworkDown, Reason: fmt.Sprintf("ICMP destination unreachable: %d", errno),
		}, true
	default:
		return NetEvent{}, false
	}
}
