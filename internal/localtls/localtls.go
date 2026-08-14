package localtls

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"sort"
	"time"
)

const (
	caCertificateName = "hideck-local-ca.crt"
	caPrivateKeyName  = "hideck-local-ca.key"
	serverCertName    = "hideck-server.crt"
	serverKeyName     = "hideck-server.key"
)

type Config struct {
	CertificateFile string
	PrivateKeyFile  string
	DataDirectory   string
}

type Result struct {
	CertificateFile   string
	PrivateKeyFile    string
	CACertificateFile string
	Managed           bool
}

func Ensure(config Config) (Result, error) {
	if config.CertificateFile != "" || config.PrivateKeyFile != "" {
		return validateProvidedPair(config)
	}
	directory := config.DataDirectory
	if directory == "" {
		directory = filepath.Join("data", "tls")
	}
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return Result{}, fmt.Errorf("create TLS data directory: %w", err)
	}
	if err := os.Chmod(directory, 0o700); err != nil {
		return Result{}, fmt.Errorf("set TLS data directory permissions: %w", err)
	}
	caCertificatePath := filepath.Join(directory, caCertificateName)
	caKeyPath := filepath.Join(directory, caPrivateKeyName)
	caCertificate, caKey, err := loadOrCreateCA(caCertificatePath, caKeyPath)
	if err != nil {
		return Result{}, err
	}
	if err := os.Chmod(caKeyPath, 0o600); err != nil {
		return Result{}, fmt.Errorf("set local CA private key permissions: %w", err)
	}
	serverCertificatePath := filepath.Join(directory, serverCertName)
	serverKeyPath := filepath.Join(directory, serverKeyName)
	if err := createServerCertificate(caCertificate, caKey, serverCertificatePath, serverKeyPath); err != nil {
		return Result{}, err
	}
	return Result{
		CertificateFile: serverCertificatePath, PrivateKeyFile: serverKeyPath,
		CACertificateFile: caCertificatePath, Managed: true,
	}, nil
}

func validateProvidedPair(config Config) (Result, error) {
	if config.CertificateFile == "" || config.PrivateKeyFile == "" {
		return Result{}, errors.New("both TLS certificate and private key files are required")
	}
	if _, err := tls.LoadX509KeyPair(config.CertificateFile, config.PrivateKeyFile); err != nil {
		return Result{}, fmt.Errorf("load configured TLS certificate: %w", err)
	}
	return Result{CertificateFile: config.CertificateFile, PrivateKeyFile: config.PrivateKeyFile}, nil
}

func loadOrCreateCA(certificatePath, keyPath string) (*x509.Certificate, *ecdsa.PrivateKey, error) {
	certificatePEM, certificateErr := os.ReadFile(certificatePath)
	keyPEM, keyErr := os.ReadFile(keyPath)
	if certificateErr == nil && keyErr == nil {
		return parseCA(certificatePEM, keyPEM)
	}
	if !errors.Is(certificateErr, os.ErrNotExist) || !errors.Is(keyErr, os.ErrNotExist) {
		return nil, nil, errors.Join(certificateErr, keyErr)
	}
	return createCA(certificatePath, keyPath)
}

func parseCA(certificatePEM, keyPEM []byte) (*x509.Certificate, *ecdsa.PrivateKey, error) {
	certificateBlock, _ := pem.Decode(certificatePEM)
	keyBlock, _ := pem.Decode(keyPEM)
	if certificateBlock == nil || keyBlock == nil {
		return nil, nil, errors.New("invalid local CA PEM data")
	}
	certificate, err := x509.ParseCertificate(certificateBlock.Bytes)
	if err != nil {
		return nil, nil, fmt.Errorf("parse local CA certificate: %w", err)
	}
	key, err := x509.ParseECPrivateKey(keyBlock.Bytes)
	if err != nil {
		return nil, nil, fmt.Errorf("parse local CA private key: %w", err)
	}
	if !certificate.IsCA || time.Now().After(certificate.NotAfter) {
		return nil, nil, errors.New("local CA certificate is expired or not a CA")
	}
	publicKey, ok := certificate.PublicKey.(*ecdsa.PublicKey)
	if !ok || !publicKey.Equal(&key.PublicKey) {
		return nil, nil, errors.New("local CA certificate and private key do not match")
	}
	if err := certificate.CheckSignatureFrom(certificate); err != nil {
		return nil, nil, fmt.Errorf("verify local CA self-signature: %w", err)
	}
	return certificate, key, nil
}

func createCA(certificatePath, keyPath string) (*x509.Certificate, *ecdsa.PrivateKey, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, nil, fmt.Errorf("generate local CA key: %w", err)
	}
	now := time.Now()
	serial, err := randomSerial()
	if err != nil {
		return nil, nil, err
	}
	template := &x509.Certificate{
		SerialNumber: serial, Subject: pkix.Name{CommonName: "HiDeck Local CA"},
		NotBefore: now.Add(-time.Hour), NotAfter: now.AddDate(10, 0, 0), IsCA: true,
		BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		return nil, nil, fmt.Errorf("create local CA certificate: %w", err)
	}
	if err := writeCertificate(certificatePath, der); err != nil {
		return nil, nil, err
	}
	if err := writeECKey(keyPath, key); err != nil {
		return nil, nil, err
	}
	return template, key, nil
}

func createServerCertificate(ca *x509.Certificate, caKey *ecdsa.PrivateKey, certificatePath, keyPath string) error {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return fmt.Errorf("generate TLS server key: %w", err)
	}
	hostnames, addresses := localNames()
	now := time.Now()
	serial, err := randomSerial()
	if err != nil {
		return err
	}
	template := &x509.Certificate{
		SerialNumber: serial, Subject: pkix.Name{CommonName: hostnames[0]},
		DNSNames: hostnames, IPAddresses: addresses, NotBefore: now.Add(-time.Hour),
		NotAfter: now.AddDate(1, 0, 0), KeyUsage: x509.KeyUsageDigitalSignature,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, template, ca, &key.PublicKey, caKey)
	if err != nil {
		return fmt.Errorf("create TLS server certificate: %w", err)
	}
	if err := writeCertificate(certificatePath, der); err != nil {
		return err
	}
	return writeECKey(keyPath, key)
}

func localNames() ([]string, []net.IP) {
	names := map[string]struct{}{"localhost": {}}
	if hostname, err := os.Hostname(); err == nil && hostname != "" {
		names[hostname] = struct{}{}
	}
	addresses := map[string]net.IP{
		"127.0.0.1": net.ParseIP("127.0.0.1"), "::1": net.ParseIP("::1"),
	}
	if interfaceAddresses, err := net.InterfaceAddrs(); err == nil {
		for _, address := range interfaceAddresses {
			if ip, _, err := net.ParseCIDR(address.String()); err == nil && !ip.IsUnspecified() {
				addresses[ip.String()] = ip
			}
		}
	}
	hostnames := make([]string, 0, len(names))
	for name := range names {
		hostnames = append(hostnames, name)
	}
	sort.Strings(hostnames)
	ips := make([]net.IP, 0, len(addresses))
	for _, address := range addresses {
		ips = append(ips, address)
	}
	return hostnames, ips
}

func randomSerial() (*big.Int, error) {
	limit := new(big.Int).Lsh(big.NewInt(1), 128)
	serial, err := rand.Int(rand.Reader, limit)
	if err != nil {
		return nil, fmt.Errorf("generate certificate serial: %w", err)
	}
	return serial, nil
}

func writeCertificate(path string, der []byte) error {
	data := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("write certificate %s: %w", path, err)
	}
	if err := os.Chmod(path, 0o644); err != nil {
		return fmt.Errorf("set certificate permissions %s: %w", path, err)
	}
	return nil
}

func writeECKey(path string, key *ecdsa.PrivateKey) error {
	der, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return fmt.Errorf("marshal EC private key: %w", err)
	}
	data := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: der})
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("write private key %s: %w", path, err)
	}
	if err := os.Chmod(path, 0o600); err != nil {
		return fmt.Errorf("set private key permissions %s: %w", path, err)
	}
	return nil
}
