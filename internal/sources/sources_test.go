package sources

import (
	"strings"
	"testing"
)

func TestKind(t *testing.T) {
	for typ, want := range map[string]string{
		"git":    "GitRepository",
		"helm":   "HelmRepository",
		"bucket": "Bucket",
		"oci":    "OCIRepository",
	} {
		got, err := Kind(typ)
		if err != nil || got != want {
			t.Fatalf("Kind(%q)=%q,%v want %q", typ, got, err, want)
		}
	}
	if _, err := Kind("nope"); err == nil {
		t.Fatal("expected error for unknown type")
	}
}

func TestGitManifest(t *testing.T) {
	req := Request{Type: TypeGit, Name: "fork", URL: "https://github.com/me/fork.git", Branch: "dev", Interval: "1m"}
	m, err := req.Manifest()
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"apiVersion: source.toolkit.fluxcd.io/v1",
		"kind: GitRepository",
		"name: fork",
		"namespace: flux-system",
		"interval: 1m",
		"branch: \"dev\"",
	} {
		if !strings.Contains(m, want) {
			t.Errorf("manifest missing %q:\n%s", want, m)
		}
	}
}

func TestBucketManifestUsesEndpoint(t *testing.T) {
	req := Request{Type: TypeBucket, Name: "assets", URL: "https://s3.example.com", BucketName: "buck", Region: "us-east-1"}
	m, err := req.Manifest()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(m, "endpoint: \"https://s3.example.com\"") {
		t.Fatalf("bucket manifest must use endpoint:\n%s", m)
	}
	if strings.Contains(m, "url:") {
		t.Fatalf("bucket manifest must not use url:\n%s", m)
	}
	if !strings.Contains(m, "bucketName: \"buck\"") {
		t.Fatalf("bucket manifest missing bucketName:\n%s", m)
	}
}

func TestValidateErrors(t *testing.T) {
	if err := (Request{Type: TypeGit, Name: "x"}).Validate(); err == nil {
		t.Error("expected missing url error")
	}
	if err := (Request{Type: TypeGit, Name: "x", URL: "u", Branch: "b", Tag: "t"}).Validate(); err == nil {
		t.Error("expected branch/tag conflict")
	}
	if err := (Request{Type: TypeBucket, Name: "x", URL: "u"}).Validate(); err == nil {
		t.Error("expected bucket-name required")
	}
}

func TestValidateSecretRef(t *testing.T) {
	req := Request{Type: TypeGit, Name: "x", URL: "u", SecretRef: "creds"}
	if err := req.ValidateSecretRef([]string{"username", "password"}, true); err != nil {
		t.Fatalf("valid keys rejected: %v", err)
	}
	if err := req.ValidateSecretRef(nil, false); err == nil {
		t.Error("expected missing secret error")
	}
	if err := req.ValidateSecretRef([]string{"foo"}, true); err == nil {
		t.Error("expected wrong-keys error")
	}
}
