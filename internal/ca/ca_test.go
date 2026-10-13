package ca

import (
	"crypto/sha1"
	"crypto/x509"
	"encoding/hex"
	"encoding/pem"
	"os"
	"strings"
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

// TestCertThumbprint verifies the SHA-1 thumbprint matches an independent
// computation over the DER certificate (the Windows store uses this format).
func TestCertThumbprint(t *testing.T) {
	store := Default(t.TempDir())
	if err := store.Generate(); err != nil {
		t.Fatal(err)
	}
	got, err := certThumbprint(store.CertFile)
	if err != nil {
		t.Fatal(err)
	}

	b, _ := os.ReadFile(store.CertFile)
	block, _ := pem.Decode(b)
	sum := sha1.Sum(block.Bytes)
	want := strings.ToUpper(hex.EncodeToString(sum[:]))
	if got != want {
		t.Errorf("thumbprint = %q want %q", got, want)
	}
	if len(got) != 40 {
		t.Errorf("thumbprint length = %d want 40", len(got))
	}
}

// TestCertCommonName verifies the CN parser used by the trust-store checks.
func TestCertCommonName(t *testing.T) {
	store := Default(t.TempDir())
	if err := store.Generate(); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(store.CertFile)
	block, _ := pem.Decode(b)
	cn, err := certCommonName(block.Bytes)
	if err != nil {
		t.Fatal(err)
	}
	if cn != CommonName {
		t.Errorf("CN = %q want %q", cn, CommonName)
	}
	if _, err := certCommonName([]byte("not a certificate")); err == nil {
		t.Error("expected an error for non-certificate bytes")
	}
}
