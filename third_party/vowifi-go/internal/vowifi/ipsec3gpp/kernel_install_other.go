//go:build !linux

package ipsec3gpp

import "errors"

func InstallPolicy(Policy) (func() error, error) {
	return nil, errors.New("ipsec3gpp: kernel policy installation is only supported on Linux")
}
