package gitops

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/micronisus/leptostack-cli/internal/cli"
	"github.com/micronisus/leptostack-cli/internal/config"
	"github.com/micronisus/leptostack-cli/internal/execx"
	"github.com/micronisus/leptostack-cli/internal/testutil"
)

func scaffoldRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	write := func(rel, content string) {
		p := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("infrastructure/kustomization.yaml",
		"apiVersion: kustomize.config.k8s.io/v1beta1\nkind: Kustomization\nresources:\n  - foo.yaml\n")
	write("apps/leptostack/overlays/example/kustomization.yaml",
		"apiVersion: kustomize.config.k8s.io/v1beta1\nkind: Kustomization\nresources:\n  - ../../base/foo.yaml\nnamePrefix: example-\n")
	write("apps/leptostack/overlays/example/naming-conf.yaml",
		"apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: naming\n")
	return dir
}

func testManager(r *config.Resolved, runner *execx.Runner) *Manager {
	return &Manager{
		R:        runner,
		IO:       cli.NewIOStreams(strings.NewReader(""), io.Discard, io.Discard),
		Resolved: r,
	}
}

func TestGenerateClusterConfigVClusterSkipsRegistryIngress(t *testing.T) {
	dir := scaffoldRepo(t)
	r := &config.Resolved{Provider: config.ProviderVCluster, ClusterName: "devops", Domain: "dev.example", LeptostackName: "portal"}

	if err := testManager(r, nil).generateClusterConfig(context.Background(), dir); err != nil {
		t.Fatal(err)
	}

	if _, err := os.Stat(filepath.Join(dir, "infrastructure", "cluster-config.yaml")); !os.IsNotExist(err) {
		t.Fatalf("cluster-config.yaml must not be created for vcluster (stat err=%v)", err)
	}
	infra, err := os.ReadFile(filepath.Join(dir, "infrastructure", "kustomization.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(infra), "cluster-config.yaml") {
		t.Fatalf("infrastructure/kustomization.yaml must not reference cluster-config.yaml for vcluster:\n%s", infra)
	}
	if _, err := os.Stat(filepath.Join(dir, "apps/leptostack/overlays/devops/kustomization.yaml")); err != nil {
		t.Fatalf("cluster overlay should still be created: %v", err)
	}
}

func TestGenerateClusterConfigMinikubeCreatesRegistryIngress(t *testing.T) {
	env := testutil.NewFakeEnv(t)
	env.Tool(t, "minikube", "case \"$*\" in\n  ip\\ *) echo 192.168.49.2 ;;\nesac\nexit 0\n")
	env.Active(t)

	dir := scaffoldRepo(t)
	r := &config.Resolved{
		Provider: config.ProviderMinikube, ClusterName: "local", Domain: "dev.example",
		LeptostackName: "portal", MinikubeProfile: "leptostack-local",
	}

	if err := testManager(r, execx.New(nil, 0)).generateClusterConfig(context.Background(), dir); err != nil {
		t.Fatal(err)
	}

	cfg, err := os.ReadFile(filepath.Join(dir, "infrastructure", "cluster-config.yaml"))
	if err != nil {
		t.Fatalf("minikube should create cluster-config.yaml: %v", err)
	}
	if !strings.Contains(string(cfg), "registry-ingress") || !strings.Contains(string(cfg), "192.168.49.100") {
		t.Fatalf("unexpected minikube cluster-config.yaml:\n%s", cfg)
	}
	infra, _ := os.ReadFile(filepath.Join(dir, "infrastructure", "kustomization.yaml"))
	if !strings.Contains(string(infra), "cluster-config.yaml") {
		t.Fatalf("minikube infra kustomization must reference cluster-config.yaml:\n%s", infra)
	}
}
