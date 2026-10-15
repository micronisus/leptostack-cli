package cmd

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/micronisus/leptostack-cli/internal/config"
	"github.com/micronisus/leptostack-cli/internal/execx"
	"github.com/micronisus/leptostack-cli/internal/kubernetes"
	"github.com/micronisus/leptostack-cli/internal/testutil"
)

// captureKubectl installs a kubectl shim that records argv and the applied
// manifest (stdin of `apply -f -`) to the fake log.
func captureKubectl(t *testing.T) *testutil.FakeEnv {
	t.Helper()
	env := newEnv(t)
	env.Tool(t, "kubectl", "echo \"kubectl $*\" >> \"$FAKE_LOG\"\ncase \"$*\" in\n  *\"apply -f -\"*) cat >> \"$FAKE_LOG\" ;;\nesac\nexit 0\n")
	return env
}

func baseSecretResolved(caFile string) *config.Resolved {
	return &config.Resolved{
		Name:        "local",
		ClusterName: "local",
		Git:         config.Git{PAT: "git-pat", CAFile: caFile},
		Base:        config.Base{PAT: "base-pat"},
	}
}

func TestCreateLeptostackBaseSecretIncludesCA(t *testing.T) {
	env := captureKubectl(t)
	caFile := filepath.Join(t.TempDir(), "ca.pem")
	if err := os.WriteFile(caFile, []byte("CA-PEM-CONTENT"), 0o600); err != nil {
		t.Fatal(err)
	}

	app, _, _ := newTestApp(t, "")
	kube := kubernetes.New(execx.New(app.IO, 0), "leptostack-local", "", "")
	if err := createLeptostackBaseSecret(context.Background(), app, baseSecretResolved(caFile), kube); err != nil {
		t.Fatal(err)
	}
	log, err := os.ReadFile(env.LogPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"ca.crt", "CA-PEM-CONTENT", "base-pat"} {
		if !strings.Contains(string(log), want) {
			t.Errorf("applied secret missing %q:\n%s", want, log)
		}
	}
}

func TestCreateLeptostackBaseSecretOmitsCAWithoutFile(t *testing.T) {
	env := captureKubectl(t)

	app, _, _ := newTestApp(t, "")
	kube := kubernetes.New(execx.New(app.IO, 0), "leptostack-local", "", "")
	if err := createLeptostackBaseSecret(context.Background(), app, baseSecretResolved(""), kube); err != nil {
		t.Fatal(err)
	}
	log, _ := os.ReadFile(env.LogPath)
	if strings.Contains(string(log), "ca.crt") {
		t.Errorf("ca.crt must be omitted without a CA file:\n%s", log)
	}
}
