package swu

import (
	"fmt"
	"sort"
	"strings"
)

type childSASnapshot struct {
	localSPI    uint32
	remoteSPI   uint32
	inboundSPIs []uint32
}

func (s *Session) captureChildSASnapshot() childSASnapshot {
	s.childSAMu.RLock()
	defer s.childSAMu.RUnlock()

	snapshot := childSASnapshot{
		localSPI: s.espLocalSPI, remoteSPI: s.espRemoteSPI,
		inboundSPIs: make([]uint32, 0, len(s.espInboundSAs)),
	}
	for spi := range s.espInboundSAs {
		snapshot.inboundSPIs = append(snapshot.inboundSPIs, spi)
	}
	sort.Slice(snapshot.inboundSPIs, func(i, j int) bool {
		return snapshot.inboundSPIs[i] < snapshot.inboundSPIs[j]
	})
	return snapshot
}

func (s childSASnapshot) inboundSPIText() string {
	values := make([]string, 0, len(s.inboundSPIs))
	for _, spi := range s.inboundSPIs {
		values = append(values, fmt.Sprintf("%08x", spi))
	}
	return strings.Join(values, ",")
}
