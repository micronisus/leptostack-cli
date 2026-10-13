//go:build linux

package ca

import (
	"os"
	"path/filepath"
	"testing"
)

// TestAnchorsDirTrusts verifies the Arch p11-kit anchors-directory check used
// by TrustStatus.
func TestAnchorsDirTrusts(t *testing.T) {
	dir := t.TempDir()
	if anchorsDirTrusts(dir) {
		t.Fatal("empty anchors dir should not report trusted")
	}

	store := Default(t.TempDir())
	if err := store.Generate(); err != nil {
		t.Fatal(err)
	}
	cert, err := os.ReadFile(store.CertFile)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "leptostack-ca.crt"), cert, 0o644); err != nil {
		t.Fatal(err)
	}
	if !anchorsDirTrusts(dir) {
		t.Fatal("anchors dir with our CA should report trusted")
	}

	other := t.TempDir()
	if err := os.WriteFile(filepath.Join(other, "other.crt"), []byte("not a pem"), 0o644); err != nil {
		t.Fatal(err)
	}
	if anchorsDirTrusts(other) {
		t.Fatal("unrelated file must not report trusted")
	}
}
