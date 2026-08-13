package localtls

import (
	"crypto/x509"
	"encoding/pem"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEnsureCreatesPersistentTrustedLocalCA(t *testing.T) {
	directory := t.TempDir()
	result, err := Ensure(Config{DataDirectory: directory})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Managed || result.CACertificateFile == "" {
		t.Fatalf("result = %+v", result)
	}
	assertFileMode(t, directory, 0o700)
	assertFileMode(t, filepath.Join(directory, caPrivateKeyName), 0o600)
	assertFileMode(t, result.PrivateKeyFile, 0o600)
	ca := readCertificate(t, result.CACertificateFile)
	server := readCertificate(t, result.CertificateFile)
	if !ca.IsCA {
		t.Fatal("generated root is not a CA")
	}
	pool := x509.NewCertPool()
	pool.AddCert(ca)
	if _, err := server.Verify(x509.VerifyOptions{Roots: pool, DNSName: "localhost"}); err != nil {
		t.Fatalf("verify localhost SAN: %v", err)
	}
	if err := server.VerifyHostname(net.IPv4(127, 0, 0, 1).String()); err != nil {
		t.Fatalf("verify loopback SAN: %v", err)
	}
	second, err := Ensure(Config{DataDirectory: directory})
	if err != nil {
		t.Fatal(err)
	}
	if readCertificate(t, second.CACertificateFile).SerialNumber.Cmp(ca.SerialNumber) != 0 {
		t.Fatal("local CA was unexpectedly replaced")
	}
}

func TestEnsureUsesProvidedCertificatePairWithoutExposingCA(t *testing.T) {
	managed, err := Ensure(Config{DataDirectory: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	provided, err := Ensure(Config{
		CertificateFile: managed.CertificateFile, PrivateKeyFile: managed.PrivateKeyFile,
		DataDirectory: t.TempDir(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if provided.Managed || provided.CACertificateFile != "" ||
		provided.CertificateFile != managed.CertificateFile {
		t.Fatalf("provided result = %+v", provided)
	}
	_, err = Ensure(Config{CertificateFile: managed.CertificateFile})
	if err == nil || !strings.Contains(err.Error(), "both TLS certificate") {
		t.Fatalf("partial pair error = %v", err)
	}
}

func TestEnsureRejectsMismatchedPersistedCAKey(t *testing.T) {
	firstDirectory, secondDirectory := t.TempDir(), t.TempDir()
	if _, err := Ensure(Config{DataDirectory: firstDirectory}); err != nil {
		t.Fatal(err)
	}
	if _, err := Ensure(Config{DataDirectory: secondDirectory}); err != nil {
		t.Fatal(err)
	}
	foreignKey, err := os.ReadFile(filepath.Join(secondDirectory, caPrivateKeyName))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(firstDirectory, caPrivateKeyName), foreignKey, 0o600); err != nil {
		t.Fatal(err)
	}
	_, err = Ensure(Config{DataDirectory: firstDirectory})
	if err == nil || !strings.Contains(err.Error(), "do not match") {
		t.Fatalf("mismatched CA error = %v", err)
	}
}

func readCertificate(t *testing.T, path string) *x509.Certificate {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	block, _ := pem.Decode(data)
	if block == nil {
		t.Fatalf("certificate %s is not PEM", path)
	}
	certificate, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatal(err)
	}
	return certificate
}

func assertFileMode(t *testing.T, path string, want os.FileMode) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != want {
		t.Fatalf("mode %s = %o, want %o", path, got, want)
	}
}
