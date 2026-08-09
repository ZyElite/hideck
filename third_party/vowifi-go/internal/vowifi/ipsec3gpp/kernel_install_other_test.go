//go:build !linux

package ipsec3gpp

import "testing"

func TestInstallPolicyReportsUnsupportedPlatform(t *testing.T) {
	cleanup, err := InstallPolicy(testPolicy(nil, nil, EncryptionAES))
	if cleanup != nil || err == nil || err.Error() != "ipsec3gpp: kernel policy installation is only supported on Linux" {
		t.Fatalf("InstallPolicy = cleanup:%t err:%v", cleanup != nil, err)
	}
}
