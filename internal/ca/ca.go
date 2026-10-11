// Package ca generates, stores, installs and exports the shared internal CA
// that serves all clusters. Generation uses crypto/x509 (no openssl shelling),
// keeping the same key/cert paths and validity as the bash implementation.
package ca

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
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

// Install adds the CA to the OS trust store for the given distro.
func (s Store) Install(ctx context.Context, r *execx.Runner, distro, domain string) error {
	switch distro {
	case "arch":
		return r.Run(ctx, execx.Cmd{Name: "sudo", Args: []string{"trust", "anchor", "--store", s.CertFile}})
	case "fedora":
		dst := fmt.Sprintf("/etc/pki/ca-trust/source/anchors/%s.crt", domain)
		if err := r.Run(ctx, execx.Cmd{Name: "sudo", Args: []string{"cp", s.CertFile, dst}}); err != nil {
			return err
		}
		if err := r.Run(ctx, execx.Cmd{Name: "sudo", Args: []string{"chmod", "644", dst}}); err != nil {
			return err
		}
		return r.Run(ctx, execx.Cmd{Name: "sudo", Args: []string{"update-ca-trust"}})
	default:
		return fmt.Errorf("unsupported Linux distribution %q", distro)
	}
}

// TrustStatus reports whether the CA appears trusted in the OS store:
// "installed", "absent", or "unknown" when it cannot be determined.
func (s Store) TrustStatus(ctx context.Context, r *execx.Runner, distro, domain string) string {
	switch distro {
	case "fedora":
		if fileNonEmpty(fmt.Sprintf("/etc/pki/ca-trust/source/anchors/%s.crt", domain)) {
			return "installed"
		}
		return "absent"
	case "arch":
		return s.archTrustStatus(ctx, r)
	default:
		return "unknown"
	}
}

// archAnchorsDir is the p11-kit trust anchors directory used by `trust anchor`.
const archAnchorsDir = "/etc/ca-certificates/trust-source/anchors"

// archTrustStatus queries the p11-kit trust store for the CA anchor, since
// `ca install` stores it there via `trust anchor --store`.
func (s Store) archTrustStatus(ctx context.Context, r *execx.Runner) string {
	// Prefer an on-disk check of the anchors directory, then fall back to the
	// trust tool listing so we don't depend on the exact stored filename.
	if anchorsDirTrusts(archAnchorsDir) {
		return "installed"
	}
	if _, ok := execx.LookPath("trust"); !ok {
		return "unknown"
	}
	out, err := r.OutputCombined(ctx, execx.Cmd{Name: "trust", Args: []string{"list"}})
	if err != nil {
		return "unknown"
	}
	if strings.Contains(out, CommonName) {
		return "installed"
	}
	return "absent"
}

// anchorsDirTrusts reports whether our CA subject appears in any file under the
// p11-kit trust anchors directory.
func anchorsDirTrusts(dir string) bool {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		b, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		if block, _ := pem.Decode(b); block != nil {
			if cert, err := x509.ParseCertificate(block.Bytes); err == nil && cert.Subject.CommonName == CommonName {
				return true
			}
		}
	}
	return false
}

func fileNonEmpty(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Size() > 0
}

// DetectDistro reads /etc/os-release and maps it to a supported distro.
func DetectDistro() (string, error) {
	b, err := os.ReadFile("/etc/os-release")
	if err != nil {
		return "", fmt.Errorf("unsupported Linux distribution")
	}
	id := parseOSReleaseID(string(b))
	switch id {
	case "fedora":
		return "fedora", nil
	case "arch":
		return "arch", nil
	}
	return "", fmt.Errorf("unsupported Linux distribution: %s (supported: Fedora, Arch Linux)", id)
}

func parseOSReleaseID(contents string) string {
	for _, line := range splitLines(contents) {
		if len(line) > 3 && line[:3] == "ID=" {
			v := line[3:]
			if len(v) >= 2 && (v[0] == '"' || v[0] == '\'') {
				v = v[1 : len(v)-1]
			}
			return v
		}
	}
	return ""
}

func splitLines(s string) []string {
	var out []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			out = append(out, s[start:i])
			start = i + 1
		}
	}
	if start < len(s) {
		out = append(out, s[start:])
	}
	return out
}
