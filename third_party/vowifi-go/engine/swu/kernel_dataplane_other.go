//go:build !linux

package swu

import "errors"

func (s *Session) setupKernelXFRMDataPlane(*childSAKeys) error {
	return errors.New("swu: XFRM data plane requires linux")
}
