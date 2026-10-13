//go:build linux

package ca

import (
	"context"
	"encoding/pem"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/micronisus/leptostack-cli/internal/execx"
)

// install adds the CA to the Fedora or Arch system trust store (via sudo).
func (s Store) install(ctx context.Context, r *execx.Runner, distro, domain string) error {
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

// trustStatus reports "installed", "absent" or "unknown" for the Linux store.
func (s Store) trustStatus(ctx context.Context, r *execx.Runner, distro, domain string) string {
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
			if cn, err := certCommonName(block.Bytes); err == nil && cn == CommonName {
				return true
			}
		}
	}
	return false
}
