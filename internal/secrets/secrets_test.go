package secrets

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"sigs.k8s.io/yaml"
)

func TestGitManifest(t *testing.T) {
	m, err := (Request{Kind: KindGit, Name: "g", Username: "me", Password: "p"}).Manifest()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(m, "kind: Secret") || !strings.Contains(m, "username: \"me\"") || !strings.Contains(m, "password: \"p\"") {
		t.Fatalf("unexpected manifest:\n%s", m)
	}
}

func TestDockerRegistryManifest(t *testing.T) {
	m, err := (Request{Kind: KindDockerRegistry, Name: "r", Registry: "ghcr.io", Username: "u", Password: "p"}).Manifest()
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Type       string            `json:"type"`
		StringData map[string]string `json:"stringData"`
	}
	if err := yaml.Unmarshal([]byte(m), &doc); err != nil {
		t.Fatalf("decode manifest: %v\n%s", err, m)
	}
	if doc.Type != "kubernetes.io/dockerconfigjson" {
		t.Fatalf("wrong type: %q", doc.Type)
	}
	var cfg struct {
		Auths map[string]struct {
			Auth     string `json:"auth"`
			Username string `json:"username"`
		} `json:"auths"`
	}
	if err := json.Unmarshal([]byte(doc.StringData[".dockerconfigjson"]), &cfg); err != nil {
		t.Fatalf("decode dockerconfigjson: %v\n%s", err, doc.StringData[".dockerconfigjson"])
	}
	want := base64.StdEncoding.EncodeToString([]byte("u:p"))
	if cfg.Auths["ghcr.io"].Auth != want {
		t.Errorf("auth = %q want %q", cfg.Auths["ghcr.io"].Auth, want)
	}
	if cfg.Auths["ghcr.io"].Username != "u" {
		t.Errorf("username = %q", cfg.Auths["ghcr.io"].Username)
	}
}

func TestTLSManifest(t *testing.T) {
	dir := t.TempDir()
	cert := filepath.Join(dir, "t.crt")
	key := filepath.Join(dir, "t.key")
	os.WriteFile(cert, []byte("CERT"), 0o600)
	os.WriteFile(key, []byte("KEY"), 0o600)
	m, err := (Request{Kind: KindTLS, Name: "t", CertFile: cert, KeyFile: key}).Manifest()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(m, "type: kubernetes.io/tls") || !strings.Contains(m, "tls.crt: \"CERT\"") || !strings.Contains(m, "tls.key: \"KEY\"") {
		t.Fatalf("unexpected tls manifest:\n%s", m)
	}
	if _, err := (Request{Kind: KindTLS, Name: "t"}).Manifest(); err == nil {
		t.Error("expected cert/key required error")
	}
}

func TestOpaqueManifest(t *testing.T) {
	m, err := (Request{Kind: KindOpaque, Name: "o", Literals: map[string]string{"a": "1"}}).Manifest()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(m, "a: \"1\"") {
		t.Fatalf("unexpected opaque manifest:\n%s", m)
	}
}
