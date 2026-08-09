package swu

import (
	"errors"
	"fmt"

	"github.com/iniwex5/vowifi-go/engine/ipsec"
)

type kernelSPIState interface {
	CurrentSPIs() (outbound uint32, inbound uint32)
}

// rekeyXFRMSA restores the original kernel rekey boundary while delegating to
// the transactional data-plane implementation used by active sessions.
func (s *Session) rekeyXFRMSA(
	oldOutSPI, oldInSPI uint32,
	newSAOut, newSAIn *ipsec.SecurityAssociation,
	encryptionID uint16,
	encryptionKeyBits int,
) error {
	plane := s.currentKernelDataPlane()
	if plane == nil {
		return nil
	}
	if oldOutSPI == 0 || oldInSPI == 0 || newSAOut == nil || newSAIn == nil {
		return errors.New("swu: XFRM rekey requires old SPIs and new SAs")
	}
	if state, ok := plane.(kernelSPIState); ok {
		currentOut, currentIn := state.CurrentSPIs()
		if currentOut != oldOutSPI || currentIn != oldInSPI {
			return fmt.Errorf(
				"swu: XFRM rekey old SPIs %08x/%08x do not match %08x/%08x",
				oldOutSPI, oldInSPI, currentOut, currentIn,
			)
		}
	}
	if encryptionID != s.espCipher || encryptionKeyBits != int(s.espEncKeyBits) {
		return fmt.Errorf("swu: XFRM rekey algorithms %d/%d do not match negotiated %d/%d",
			encryptionID, encryptionKeyBits, s.espCipher, s.espEncKeyBits)
	}
	runtime := s.legacyXFRMChildRuntime(newSAOut, newSAIn)
	return plane.Rekey(s, runtime)
}

func (s *Session) legacyXFRMChildRuntime(
	newSAOut, newSAIn *ipsec.SecurityAssociation,
) *childSARuntime {
	s.childSAMu.RLock()
	defer s.childSAMu.RUnlock()
	return &childSARuntime{
		outbound: newSAOut, inbound: newSAIn,
		localSPI: newSAIn.SPI, remoteSPI: newSAOut.SPI,
		ni: append([]byte(nil), s.childNi...), nr: append([]byte(nil), s.childNr...),
		tsi: cloneTrafficSelectorPayload(s.childTSi), tsr: cloneTrafficSelectorPayload(s.childTSr),
		outboundKeys: childDirectionKeys{
			enc:   append([]byte(nil), newSAOut.EncryptionKey...),
			integ: append([]byte(nil), newSAOut.IntegrityKey...),
		},
		inboundKeys: childDirectionKeys{
			enc:   append([]byte(nil), newSAIn.EncryptionKey...),
			integ: append([]byte(nil), newSAIn.IntegrityKey...),
		},
		dh: s.childDH, dhSecret: append([]byte(nil), s.childDHSecret...),
	}
}
