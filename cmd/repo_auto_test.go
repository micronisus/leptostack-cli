package cmd

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/micronisus/leptostack-cli/internal/testutil"
)

const autoConfig = `apiVersion: v1
kind: LeptostackConfig
current-context: local
clusters:
  local:
    provider: minikube
    clusterName: local
    leptostackName: portal
    domain: alice-leptostack.test
    minikube:
      cpus: 8
      memoryMB: 24576
git:
  server: github
  hostname: github.com
  branch: main
  clusterPath: clusters/{cluster}
  repository: auto
template:
  url: https://github.com/leptostack-fluxcd/cluster-template.git
  branch: main
`

const ghAutoShim = `echo "gh $*" >> "$FAKE_LOG"
case "$*" in
  "api user --jq .login") echo alice ;;
  "auth token") echo gho_testtoken ;;
  "api repos/alice/local-gitops") echo 'Error: HTTP 404: Not Found' >&2; exit 1 ;;
  "api --method POST user/repos"*) echo '{"full_name":"alice/local-gitops","clone_url":"https://github.com/alice/local-gitops.git"}' ;;
esac
exit 0
`

const mismatchedClusterConfig = `apiVersion: v1
kind: LeptostackConfig
current-context: local
clusters:
  local:
    provider: minikube
    clusterName: foo
    leptostackName: portal
    domain: alice.test
    minikube:
      cpus: 8
      memoryMB: 24576
git:
  server: github
  repository: auto
  branch: main
  clusterPath: clusters/foo
template:
  url: https://github.com/leptostack-fluxcd/cluster-template.git
  branch: main
`

func TestConfigInitUsesClusterNameAsKey(t *testing.T) {
	newEnv(t)
	cfgPath := testutil.SetHomeConfig(t, "")

	_, errOut, code := runCLI(t, "",
		"config", "init", "--non-interactive", "--provider", "minikube",
		"--cluster-name", "foo", "--domain", "alice.test", "--git-server", "github",
	)
	if code != 0 {
		t.Fatalf("exit code = %d (stderr=%s)", code, errOut)
	}
	b, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	cfg := string(b)
	if !strings.Contains(cfg, "  foo:") || !strings.Contains(cfg, "clusterName: foo") {
		t.Fatalf("expected the cluster name to be used as the key:\n%s", cfg)
	}
	if strings.Contains(cfg, "  local:") {
		t.Fatalf("cluster name was ignored and keyed as local:\n%s", cfg)
	}

	ctx, _, code := runCLI(t, "", "get", "context", "-o", "name")
	if code != 0 || strings.TrimSpace(ctx) != "leptostack-foo" {
		t.Fatalf("context = %q (code %d), want leptostack-foo", ctx, code)
	}
}

func TestConfigInitRenamesMismatchedKeyToClusterName(t *testing.T) {
	newEnv(t)
	cfgPath := testutil.SetHomeConfig(t, mismatchedClusterConfig)

	_, errOut, code := runCLI(t, "",
		"config", "init", "--non-interactive", "--provider", "minikube",
		"--domain", "alice.test", "--git-server", "github",
	)
	if code != 0 {
		t.Fatalf("exit code = %d (stderr=%s)", code, errOut)
	}
	b, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	cfg := string(b)
	if strings.Contains(cfg, "  local:") {
		t.Fatalf("mismatched key was not renamed to the cluster name:\n%s", cfg)
	}
	if !strings.Contains(cfg, "  foo:") {
		t.Fatalf("expected the cluster to be keyed as foo:\n%s", cfg)
	}
}

func TestConfigInitDefaultsToAutoRepository(t *testing.T) {
	newEnv(t)
	cfgPath := testutil.SetHomeConfig(t, "")

	_, errOut, code := runCLI(t, "",
		"config", "init", "--non-interactive", "--provider", "minikube",
		"--cluster", "local", "--domain", "alice.test", "--git-server", "github",
	)
	if code != 0 {
		t.Fatalf("exit code = %d (stderr=%s)", code, errOut)
	}
	b, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "repository: auto") {
		t.Fatalf("expected repository: auto in config:\n%s", b)
	}
}

func TestProvisionAutoRepository(t *testing.T) {
	env := newEnv(t)
	env.Tool(t, "gh", ghAutoShim)
	cfgPath := testutil.SetHomeConfig(t, autoConfig)

	app, _, _ := newTestApp(t, "")
	r, err := app.Resolve()
	if err != nil {
		t.Fatal(err)
	}
	if !r.AutoRepo {
		t.Fatal("expected AutoRepo to be true before provisioning")
	}

	if err := provisionAutoRepository(context.Background(), app, r); err != nil {
		t.Fatal(err)
	}
	if r.AutoRepo {
		t.Error("AutoRepo should be false after provisioning")
	}
	if r.Git.Repository != "local-gitops" {
		t.Errorf("repository = %q want local-gitops", r.Git.Repository)
	}
	if r.Git.Owner != "alice" {
		t.Errorf("owner = %q want alice", r.Git.Owner)
	}
	if r.Git.PAT != "gho_testtoken" {
		t.Errorf("push token = %q", r.Git.PAT)
	}
	if !env.HasInvocation(t, "--method POST user/repos") {
		t.Fatalf("expected repository creation; got %v", env.Invocations(t))
	}

	b, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"local-gitops", "gho_testtoken", "owner: alice"} {
		if !strings.Contains(string(b), want) {
			t.Errorf("config missing %q:\n%s", want, b)
		}
	}
}

func TestProvisionAutoRepositorySkipsWhenConfigured(t *testing.T) {
	env := newEnv(t)
	env.Tool(t, "gh", ghAutoShim)
	testutil.SetHomeConfig(t, testConfig) // repository: leptostack-fluxcd (not auto)

	app, _, _ := newTestApp(t, "")
	r, err := app.Resolve()
	if err != nil {
		t.Fatal(err)
	}
	if r.AutoRepo {
		t.Fatal("did not expect AutoRepo for a concrete repository")
	}
	if err := provisionAutoRepository(context.Background(), app, r); err != nil {
		t.Fatal(err)
	}
	if env.HasInvocation(t, "--method POST user/repos") {
		t.Fatalf("provisioning must be a no-op for a concrete repository; got %v", env.Invocations(t))
	}
}
