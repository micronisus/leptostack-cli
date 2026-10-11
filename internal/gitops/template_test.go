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

const templateClusterKustomization = `apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization

resources:
    - flux-system
    - ../../infrastructure
    - ../../apps/leptostack/overlays/example

patches:
  - target:
      kind: Kustomization
      name: keycloak-crd
      namespace: flux-system
    patch: |-
      - op: add
        path: "/spec/patches"
        value:
          - target:
              group: rbac.authorization.k8s.io
              version: v1
              kind: ClusterRoleBinding
              name: keycloak-operator-clusterrole-binding
            patch: |
              - op: add
                path: "/subjects"
                value:
                  - kind: ServiceAccount
                    name: keycloak-operator
                    namespace: example-keycloak
  - target:
      kind: Kustomization
      name: infra-config
      namespace: flux-system
    patch: |-
      - op: add
        path: "/spec/patches"
        value:
          - target:
              group: trust.cert-manager.io
              version: v1alpha1
              kind: Bundle
              name: internal-ca-bundle
            patch: |
              - op: add
                path: "/spec/target/namespaceSelector"
                value:
                  matchExpressions:
                    - key: app.kubernetes.io/part-of
                      operator: In
                      values:
                        - leptostack-example
                        - leptostack-module-example
`

func TestAddInternalCADeletePatchTargetsInfraConfig(t *testing.T) {
	p := filepath.Join(t.TempDir(), "kustomization.yaml")
	if err := os.WriteFile(p, []byte(templateClusterKustomization), 0o644); err != nil {
		t.Fatal(err)
	}

	changed, err := addInternalCADeletePatch(p)
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Fatal("expected the delete patch to be added (the internal-ca-bundle name must not block it)")
	}

	out, _ := os.ReadFile(p)
	s := string(out)
	idx := func(sub string) int { return strings.Index(s, sub) }
	iKeycloak, iInfra := idx("name: keycloak-crd"), idx("name: infra-config")
	iDelete, iBundle := idx("$patch: delete"), idx("name: internal-ca-bundle")
	if iInfra < 0 || iDelete < 0 || iBundle < 0 {
		t.Fatalf("missing markers (infra=%d delete=%d bundle=%d):\n%s", iInfra, iDelete, iBundle, s)
	}
	if !(iInfra < iDelete && iDelete < iBundle) {
		t.Fatalf("delete patch must be inserted under infra-config before the Bundle patch:\n%s", s)
	}
	if strings.Contains(s[iKeycloak:iInfra], "$patch: delete") {
		t.Fatalf("delete patch must not land under keycloak-crd:\n%s", s)
	}

	changedAgain, err := addInternalCADeletePatch(p)
	if err != nil {
		t.Fatal(err)
	}
	if changedAgain {
		t.Fatal("second run must be idempotent")
	}
}
