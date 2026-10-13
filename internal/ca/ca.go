// Package ca generates, stores, installs and exports the shared internal CA
// that serves all clusters. Generation uses crypto/x509 (no openssl shelling),
// keeping the same key/cert paths and validity as the bash implementation.
//
// OS-specific trust-store integration lives in build-tagged files (detect_*.go
// and trust_*.go); the Store/Generate/PEM logic and the certificate parsers
// here are platform-neutral.
package ca

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha1"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/micronisus/leptostack-cli/internal/execx"
)

// ValidityDays matches CA_VALIDITY_DAYS in the bash implementation.
const ValidityDays = 3650

// CommonName is the subject CN of the internal CA.
const CommonName = "LeptoStack Internal CA"

// Store is the on-disk CA location.
type Store struct {
	CertFile string
	KeyFile  string
}

// Default returns the CA store under the config directory.
func Default(configDir string) Store {
	return Store{
		CertFile: filepath.Join(configDir, "leptostack-ca.crt"),
		KeyFile:  filepath.Join(configDir, "leptostack-ca.key"),
	}
}

// Exists reports whether both the cert and key are present and non-empty.
func (s Store) Exists() bool {
	return fileNonEmpty(s.CertFile) && fileNonEmpty(s.KeyFile)
}

// Ensure generates the CA if it does not already exist.
func (s Store) Ensure() error {
	if s.Exists() {
		return nil
	}
	return s.Generate()
}

// Generate creates a new self-signed EC P-256 CA certificate and key.
func (s Store) Generate() error {
	if err := os.MkdirAll(filepath.Dir(s.CertFile), 0o700); err != nil {
		return err
	}

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return err
	}

	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return err
	}

	now := time.Now()
	tmpl := &x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: CommonName},
		NotBefore:             now.Add(-time.Hour),
		NotAfter:              now.AddDate(0, 0, ValidityDays),
		IsCA:                  true,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
	}

	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		return err
	}

	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return err
	}

	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})

	if err := os.WriteFile(s.KeyFile, keyPEM, 0o600); err != nil {
		return err
	}
	if err := os.WriteFile(s.CertFile, certPEM, 0o644); err != nil {
		return err
	}
	return nil
}

// PEM returns the CA certificate PEM bytes.
func (s Store) PEM() ([]byte, error) {
	return os.ReadFile(s.CertFile)
}

// Install adds the CA to the OS trust store for the given distro ("fedora",
// "arch" or "windows"). The platform-specific behavior lives in trust_*.go.
func (s Store) Install(ctx context.Context, r *execx.Runner, distro, domain string) error {
	return s.install(ctx, r, distro, domain)
}

// TrustStatus reports whether the CA appears trusted in the OS store:
// "installed", "absent", or "unknown" when it cannot be determined.
func (s Store) TrustStatus(ctx context.Context, r *execx.Runner, distro, domain string) string {
	return s.trustStatus(ctx, r, distro, domain)
}

// certThumbprint returns the SHA-1 thumbprint (uppercase hex, no separators) of
// the certificate at path, matching the format used by the Windows certificate
// store. The PEM wrapper is stripped when present.
func certThumbprint(path string) (string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	der := b
	if block, _ := pem.Decode(b); block != nil {
		der = block.Bytes
	}
	sum := sha1.Sum(der)
	return strings.ToUpper(hex.EncodeToString(sum[:])), nil
}

// certCommonName parses a DER certificate and returns its subject CN.
func certCommonName(der []byte) (string, error) {
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		return "", err
	}
	return cert.Subject.CommonName, nil
}

func fileNonEmpty(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Size() > 0
}
