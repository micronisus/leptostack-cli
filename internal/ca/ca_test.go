package ca

import (
	"crypto/x509"
	"encoding/pem"
	"os"
	"path/filepath"
	"testing"
)

func TestGenerateAndEnsure(t *testing.T) {
	dir := t.TempDir()
	store := Default(dir)

	if store.Exists() {
		t.Fatal("fresh store should not exist")
	}
	if err := store.Generate(); err != nil {
		t.Fatal(err)
	}
	if !store.Exists() {
		t.Fatal("store should exist after generate")
	}

	// Cert must parse and be a CA with the expected CN.
	b, err := os.ReadFile(store.CertFile)
	if err != nil {
		t.Fatal(err)
	}
	block, _ := pem.Decode(b)
	if block == nil {
		t.Fatal("cert is not valid PEM")
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatal(err)
	}
	if !cert.IsCA {
		t.Error("generated certificate is not a CA")
	}
	if cert.Subject.CommonName != "LeptoStack Internal CA" {
		t.Errorf("CN = %q", cert.Subject.CommonName)
	}

	// Key file must exist; re-Ensure must not regenerate.
	keyBefore, _ := os.ReadFile(store.KeyFile)
	if err := store.Ensure(); err != nil {
		t.Fatal(err)
	}
	keyAfter, _ := os.ReadFile(store.KeyFile)
	if string(keyBefore) != string(keyAfter) {
		t.Error("Ensure regenerated an existing CA")
	}
}

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
