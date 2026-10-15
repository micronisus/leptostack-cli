package cmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/micronisus/leptostack-cli/internal/cli"
	"github.com/micronisus/leptostack-cli/internal/testutil"
	versionpkg "github.com/micronisus/leptostack-cli/internal/version"
)

const testConfig = `apiVersion: v1
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
      owner: micronisus
      repository: leptostack-fluxcd
      branch: main
      clusterPath: clusters/{cluster}
      pat: ghp_x
    registry:
      repository: ghcr.io
      credentials: u:p
    base:
      url: https://github.com/leptostack-fluxcd/leptostack-base.git
      branch: main
`

const kustomizationsJSON = `{"items":[
 {"metadata":{"name":"flux-system","creationTimestamp":"2026-01-01T00:00:00Z"},"spec":{"suspend":false},"status":{"conditions":[{"type":"Ready","status":"True"}]}},
 {"metadata":{"name":"apps","creationTimestamp":"2026-01-01T00:00:00Z"},"spec":{"suspend":true},"status":{"conditions":[{"type":"Ready","status":"False","message":"paused"}]}}
]}`

const kubectlShim = `echo "kubectl $*" >> "$FAKE_LOG"
case "$*" in
  *"get --raw /healthz"*) echo ok ;;
  *"config get-contexts -o name"*) echo "leptostack-local" ;;
  *"config current-context"*) echo "leptostack-local" ;;
  *"get kustomizations.kustomize.toolkit.fluxcd.io apps"*) echo '{"spec":{"suspend":false},"status":{"conditions":[{"type":"Ready","status":"False"}]}}' ;;
  *"get kustomizations.kustomize.toolkit.fluxcd.io"*) cat "$FAKE_KUSTOMIZATIONS" ;;
esac
exit 0
`

const fluxShim = `echo "flux $*" >> "$FAKE_LOG"
case "$*" in
  *"version"*) echo "flux version 2.9.6" ;;
esac
exit 0
`

const minikubeShim = `echo "minikube $*" >> "$FAKE_LOG"
case "$*" in
  ip\ *) echo 192.168.49.2 ;;
esac
exit 0
`

func newEnv(t *testing.T) *testutil.FakeEnv {
	env := testutil.NewFakeEnv(t)
	env.Tool(t, "kubectl", kubectlShim)
	env.Tool(t, "flux", fluxShim)
	env.Tool(t, "minikube", minikubeShim)
	env.Active(t)
	t.Setenv("FAKE_KUSTOMIZATIONS", writeFile(t, "kustomizations.json", kustomizationsJSON))
	return env
}

func writeFile(t *testing.T, name, content string) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func newTestApp(t *testing.T, input string) (*cli.App, *bytes.Buffer, *bytes.Buffer) {
	t.Helper()
	var out, errb bytes.Buffer
	streams := cli.NewIOStreams(strings.NewReader(input), &out, &errb)
	flags := &cli.GlobalFlags{}
	app := cli.New(streams, flags, versionpkg.Info{Version: "test"})
	return app, &out, &errb
}

func runCLI(t *testing.T, input string, args ...string) (stdout, stderr string, code int) {
	t.Helper()
	app, out, errb := newTestApp(t, input)
	root := NewRootCmd(app)
	root.SetArgs(args)
	root.SetOut(out)
	root.SetErr(errb)
	err := root.Execute()
	if err != nil {
		fmt.Fprintf(errb, "Error: %v\n", err)
	}
	return out.String(), errb.String(), exitCodeFor(err)
}

func TestNoConfigExitCode(t *testing.T) {
	newEnv(t)
	testutil.SetHomeConfig(t, "")
	_, _, code := runCLI(t, "", "get", "context")
	if code != cli.ExitConfig {
		t.Fatalf("exit code = %d want %d", code, cli.ExitConfig)
	}
}

func TestUsageExitCodes(t *testing.T) {
	newEnv(t)
	testutil.SetHomeConfig(t, testConfig)
	if _, _, code := runCLI(t, "", "port-forward"); code != cli.ExitUsage {
		t.Errorf("missing arg exit = %d want %d", code, cli.ExitUsage)
	}
	if _, _, code := runCLI(t, "", "frobnicate"); code != cli.ExitUsage {
		t.Errorf("unknown command exit = %d want %d", code, cli.ExitUsage)
	}
	if _, _, code := runCLI(t, "", "version", "--nope"); code != cli.ExitUsage {
		t.Errorf("bad flag exit = %d want %d", code, cli.ExitUsage)
	}
}

func TestWaitTimeoutExitCode(t *testing.T) {
	newEnv(t)
	testutil.SetHomeConfig(t, testConfig)
	_, _, code := runCLI(t, "", "wait", "kustomization", "apps", "--ready", "--timeout", "1s")
	if code != cli.ExitTimeout {
		t.Fatalf("exit code = %d want %d", code, cli.ExitTimeout)
	}
}

func TestGetClustersName(t *testing.T) {
	newEnv(t)
	testutil.SetHomeConfig(t, testConfig)
	out, _, code := runCLI(t, "", "get", "clusters", "-o", "name")
	if code != 0 {
		t.Fatalf("exit code = %d", code)
	}
	if strings.TrimSpace(out) != "local" {
		t.Fatalf("expected bare cluster names, got %q", out)
	}
	if strings.Contains(out, "{") {
		t.Fatalf("-o name must not emit JSON: %q", out)
	}
}

func TestDeleteSecretRequiresConfirmation(t *testing.T) {
	newEnv(t)
	testutil.SetHomeConfig(t, testConfig)
	// Non-interactive and no --yes must fail with a usage error before deleting.
	_, _, code := runCLI(t, "", "delete", "secret", "foo")
	if code != cli.ExitUsage {
		t.Fatalf("exit code = %d want %d", code, cli.ExitUsage)
	}
}

func TestDeleteSecretDryRun(t *testing.T) {
	env := newEnv(t)
	testutil.SetHomeConfig(t, testConfig)
	_, errOut, code := runCLI(t, "", "delete", "secret", "foo", "--dry-run")
	if code != 0 {
		t.Fatalf("exit code = %d", code)
	}
	if !strings.Contains(errOut, "Would delete secret foo") {
		t.Fatalf("expected dry-run message, got %q", errOut)
	}
	if env.HasInvocation(t, "delete secret") {
		t.Fatalf("dry-run must not delete; got %v", env.Invocations(t))
	}
}

func TestSyncClusterDryRun(t *testing.T) {
	newEnv(t)
	testutil.SetHomeConfig(t, testConfig)
	_, errOut, code := runCLI(t, "", "sync", "cluster", "--force", "--dry-run")
	if code != 0 {
		t.Fatalf("exit code = %d", code)
	}
	if !strings.Contains(errOut, "Would sync the embedded cluster template") {
		t.Fatalf("expected dry-run message, got %q", errOut)
	}
}

func TestGetContext(t *testing.T) {
	newEnv(t)
	testutil.SetHomeConfig(t, testConfig)
	out, _, code := runCLI(t, "", "get", "context", "-o", "name")
	if code != 0 {
		t.Fatalf("exit code = %d", code)
	}
	if strings.TrimSpace(out) != "leptostack-local" {
		t.Fatalf("context = %q", out)
	}
}

func TestGetClustersJSON(t *testing.T) {
	newEnv(t)
	testutil.SetHomeConfig(t, testConfig)
	out, _, code := runCLI(t, "", "get", "clusters", "-o", "json")
	if code != 0 {
		t.Fatalf("exit code = %d", code)
	}
	var infos []clusterInfo
	if err := json.Unmarshal([]byte(out), &infos); err != nil {
		t.Fatalf("decode clusters json: %v\n%s", err, out)
	}
	if len(infos) != 1 {
		t.Fatalf("expected 1 cluster, got %d", len(infos))
	}
	if infos[0].Name != "local" || infos[0].Provider != "minikube" || infos[0].State != "running" {
		t.Fatalf("unexpected cluster: %+v", infos[0])
	}
	if infos[0].Kustomizations.Total != 2 || infos[0].Kustomizations.Ready != 1 || infos[0].Kustomizations.Suspended != 1 {
		t.Fatalf("unexpected kustomization counts: %+v", infos[0].Kustomizations)
	}
}

func TestStatusJSON(t *testing.T) {
	newEnv(t)
	testutil.SetHomeConfig(t, testConfig)
	out, _, code := runCLI(t, "", "status", "-o", "json")
	if code != 0 {
		t.Fatalf("exit code = %d", code)
	}
	var st statusOutput
	if err := json.Unmarshal([]byte(out), &st); err != nil {
		t.Fatalf("decode status: %v\n%s", err, out)
	}
	if st.Flux.Kustomizations.Total != 2 || st.Flux.Kustomizations.Ready != 1 || st.Flux.Kustomizations.Suspended != 1 {
		t.Fatalf("unexpected flux status: %+v", st.Flux)
	}
	if st.Endpoint.GatewayIP != "192.168.49.100" {
		t.Fatalf("gateway ip = %q", st.Endpoint.GatewayIP)
	}
}

func TestCreateSecretDryRun(t *testing.T) {
	newEnv(t)
	testutil.SetHomeConfig(t, testConfig)
	out, _, code := runCLI(t, "", "create", "secret", "git", "helm-private", "--username", "me", "--password", "s3cr3t", "--dry-run")
	if code != 0 {
		t.Fatalf("exit code = %d", code)
	}
	if !strings.Contains(out, "name: helm-private") || !strings.Contains(out, `password: "s3cr3t"`) {
		t.Fatalf("unexpected manifest:\n%s", out)
	}
}

func TestCreateSourceGitDryRun(t *testing.T) {
	newEnv(t)
	testutil.SetHomeConfig(t, testConfig)
	out, _, code := runCLI(t, "", "create", "source", "git", "fork", "--url", "https://github.com/me/fork.git", "--branch", "dev", "--dry-run")
	if code != 0 {
		t.Fatalf("exit code = %d", code)
	}
	if !strings.Contains(out, "kind: GitRepository") || !strings.Contains(out, "name: fork") {
		t.Fatalf("unexpected manifest:\n%s", out)
	}
}

func TestEnableKustomizationPatches(t *testing.T) {
	env := newEnv(t)
	testutil.SetHomeConfig(t, testConfig)
	_, _, code := runCLI(t, "", "enable", "kustomization", "apps")
	if code != 0 {
		t.Fatalf("exit code = %d", code)
	}
	if !env.HasInvocation(t, "patch kustomizations/apps") {
		t.Fatalf("expected patch invocation; got %v", env.Invocations(t))
	}
	if !env.HasInvocation(t, `{"spec":{"suspend":false}}`) {
		t.Fatalf("expected suspend=false payload; got %v", env.Invocations(t))
	}
}

func TestDisableKustomizationPatches(t *testing.T) {
	env := newEnv(t)
	testutil.SetHomeConfig(t, testConfig)
	_, _, code := runCLI(t, "", "disable", "kustomization", "apps")
	if code != 0 {
		t.Fatalf("exit code = %d", code)
	}
	if !env.HasInvocation(t, `{"spec":{"suspend":true}}`) {
		t.Fatalf("expected suspend=true payload; got %v", env.Invocations(t))
	}
}

func TestConfigInitNonInteractive(t *testing.T) {
	newEnv(t)
	path := testutil.SetHomeConfig(t, "")
	_, errOut, code := runCLI(t, "ghp_token\n",
		"config", "init", "--non-interactive", "--provider", "minikube",
		"--cluster-name", "local", "--domain", "alice.test",
		"--repo-url", "https://github.com/acme/repo", "--git-pat-stdin",
	)
	if code != 0 {
		t.Fatalf("exit code = %d (stderr=%s)", code, errOut)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "ghp_token") || !strings.Contains(string(b), "owner: acme") {
		t.Fatalf("config not written correctly:\n%s", b)
	}
}

func TestConfigMigrationOnFirstRun(t *testing.T) {
	newEnv(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_RUNTIME_DIR", filepath.Join(home, "run"))
	cfgDir := filepath.Join(home, ".config", "leptostack")
	if err := os.MkdirAll(cfgDir, 0o700); err != nil {
		t.Fatal(err)
	}
	legacy := "CLUSTER_PROVIDER=\"minikube\"\nCLUSTER_NAME=\"local\"\nLEPTOSTACK_DOMAIN=\"legacy.test\"\nGIT_OWNER=\"o\"\nGIT_REPO=\"r\"\nGIT_BRANCH=\"main\"\n"
	if err := os.WriteFile(filepath.Join(cfgDir, "config"), []byte(legacy), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("LEPTOSTACK_CONFIG", filepath.Join(cfgDir, "config.yaml"))

	out, errOut, code := runCLI(t, "", "get", "context", "-o", "name")
	if code != 0 {
		t.Fatalf("exit code = %d (stderr=%s)", code, errOut)
	}
	if strings.TrimSpace(out) != "leptostack-local" {
		t.Fatalf("context = %q", out)
	}
	if _, err := os.Stat(filepath.Join(cfgDir, "config.yaml")); err != nil {
		t.Fatalf("migrated YAML config missing: %v", err)
	}
	if !strings.Contains(errOut, "Imported legacy configuration") {
		t.Fatalf("expected migration note, got %q", errOut)
	}
	if _, err := os.Stat(filepath.Join(cfgDir, "config")); err != nil {
		t.Fatalf("original legacy config should be kept: %v", err)
	}
}
