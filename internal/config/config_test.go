package config

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

const legacyKV = `GIT_SERVER="github"
GIT_PAT="ghp_secret"
GIT_HOSTNAME="github.com"
GIT_OWNER="micronisus"
GIT_REPO="leptostack-fluxcd"
GIT_BRANCH="main"
CLUSTER_PATH="clusters/local"
CLUSTER_PROVIDER="minikube"
CLUSTER_NAME="local"
LEPTOSTACK_NAME="portal"
LEPTOSTACK_DOMAIN="alice-leptostack.test"
MINIKUBE_CPUS="8"
MINIKUBE_MEMORY="24576"
RESOURCES_GIT_PAT="res_secret"
MODULE_REGISTRY_REPOSITORY="ghcr.io"
MODULE_REGISTRY_CREDENTIALS="user:pass"
`

func TestImportLegacy(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_RUNTIME_DIR", dir)
	path := filepath.Join(dir, "config")
	if err := os.WriteFile(path, []byte(legacyKV), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, err := ImportLegacy(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.CurrentContext != "local" {
		t.Errorf("current-context = %q", cfg.CurrentContext)
	}
	cl := cfg.Clusters["local"]
	if cl == nil {
		t.Fatal("cluster local missing")
	}
	if cl.Provider != ProviderMinikube || cl.ClusterName != "local" || cl.Domain != "alice-leptostack.test" {
		t.Errorf("unexpected cluster: %+v", cl)
	}
	if cl.Minikube.CPUs != 8 || cl.Minikube.MemoryMB != 24576 {
		t.Errorf("minikube resources: %+v", cl.Minikube)
	}
	if cfg.Git.PAT != "ghp_secret" || cfg.Git.Owner != "micronisus" {
		t.Errorf("git: %+v", cfg.Git)
	}
	if cfg.Base.PAT != "res_secret" {
		t.Errorf("base pat = %q", cfg.Base.PAT)
	}
	if cfg.Registry.Credentials != "user:pass" {
		t.Errorf("registry creds = %q", cfg.Registry.Credentials)
	}
}

func TestResolveEnvOverrides(t *testing.T) {
	cfg := &Config{
		APIVersion:     APIVersion,
		Kind:           Kind,
		CurrentContext: "local",
		Clusters: map[string]*Cluster{
			"local": {Provider: ProviderMinikube, ClusterName: "local", Domain: "x.test", Minikube: &Minikube{CPUs: 4, MemoryMB: 8192}},
		},
		Git:      &Git{PAT: "from-file", Branch: "main", Owner: "o", Repository: "r", ClusterPath: "clusters/{cluster}"},
		Registry: &Registry{Repository: "ghcr.io", Credentials: "file:creds"},
		Base:     &Base{URL: "u", Branch: "main"},
	}
	t.Setenv(EnvGitPAT, "from-env")
	t.Setenv(EnvRegistryCredentials, "env:creds")

	r, err := cfg.Resolve("")
	if err != nil {
		t.Fatal(err)
	}
	if r.Git.PAT != "from-env" {
		t.Errorf("git pat env override failed: %q", r.Git.PAT)
	}
	if r.Registry.Credentials != "env:creds" {
		t.Errorf("registry env override failed: %q", r.Registry.Credentials)
	}
	if r.Minikube.CPUs != 4 {
		t.Errorf("cpus = %d", r.Minikube.CPUs)
	}
	if r.KubeContext != "leptostack-local" {
		t.Errorf("context = %q", r.KubeContext)
	}
	if r.ClonePath() != "clusters/local" {
		t.Errorf("clone path = %q", r.ClonePath())
	}
}

func TestValidateRejectsBadProvider(t *testing.T) {
	cfg := &Config{APIVersion: APIVersion, Kind: Kind, Clusters: map[string]*Cluster{"x": {Provider: "docker"}}}
	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected validation error")
	}
	var ve *ValidationError
	if !asValidation(err, &ve) || !strings.Contains(ve.Path, "provider") {
		t.Fatalf("wrong error: %v", err)
	}
}

func TestRedacted(t *testing.T) {
	cfg := &Config{
		Git:      &Git{PAT: "secret"},
		Registry: &Registry{Credentials: "user:pass"},
		Base:     &Base{PAT: "tsecret"},
		Clusters: map[string]*Cluster{"local": {Provider: ProviderMinikube}},
	}
	red := cfg.Redacted()
	if red.Git.PAT != "<redacted>" || red.Registry.Credentials != "<redacted>" || red.Base.PAT != "<redacted>" {
		t.Fatalf("redaction failed: %+v", red)
	}
	if cfg.Git.PAT != "secret" {
		t.Error("original config mutated by Redacted")
	}
}

func TestDirHonorsEnvOverride(t *testing.T) {
	t.Setenv(EnvConfigDir, "/mnt/leptostack-ca")
	if got := Dir(); got != "/mnt/leptostack-ca" {
		t.Fatalf("Dir = %q, want the LEPTOSTACK_CONFIG_DIR override", got)
	}
	if got := DefaultPath(); got != filepath.Join("/mnt/leptostack-ca", "config.yaml") {
		t.Fatalf("DefaultPath = %q", got)
	}
	if got := LegacyPath(); got != filepath.Join("/mnt/leptostack-ca", "config") {
		t.Fatalf("LegacyPath = %q", got)
	}
}

func TestDirFallsBackWithoutEnvOverride(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix HOME-based default")
	}
	t.Setenv(EnvConfigDir, "")
	home := t.TempDir()
	t.Setenv("HOME", home)
	got := Dir()
	if got == "" || got == home {
		t.Fatalf("Dir = %q, expected a path under HOME", got)
	}
	if !strings.HasPrefix(got, home) {
		t.Fatalf("Dir = %q, want it under HOME %q", got, home)
	}
}

func asValidation(err error, target **ValidationError) bool {
	ve, ok := err.(*ValidationError)
	if ok {
		*target = ve
	}
	return ok
}

func TestLoadFromStripsDeprecatedDNSTargetIP(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	doc := `apiVersion: v1
kind: LeptostackConfig
current-context: vc
clusters:
  vc:
    provider: vcluster
    clusterName: vc
    domain: vc.test
    vcluster:
      hostContext: kind-baremetal
      hostNamespace: alice
      dnsTargetIP: 10.40.0.100
git:
  server: github
  repository: auto
  branch: main
base:
  url: https://github.com/leptostack-fluxcd/leptostack-base.git
  branch: main
`
	if err := os.WriteFile(path, []byte(doc), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadFrom(path)
	if err != nil {
		t.Fatalf("config with the deprecated dnsTargetIP should still load: %v", err)
	}
	r, err := cfg.Resolve("vc")
	if err != nil {
		t.Fatal(err)
	}
	if r.VCluster.HostNamespace != "alice" || r.Provider != ProviderVCluster {
		t.Fatalf("unexpected resolved vcluster: %+v", r.VCluster)
	}
}

func TestLoadFromMigratesTemplateToBase(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	doc := `apiVersion: v1
kind: LeptostackConfig
current-context: local
clusters:
  local:
    provider: minikube
    clusterName: local
    domain: local.test
git:
  server: github
  repository: auto
  branch: main
template:
  url: https://github.com/leptostack-fluxcd/cluster-template.git
  branch: develop
  pat: old_secret
`
	if err := os.WriteFile(path, []byte(doc), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadFrom(path)
	if err != nil {
		t.Fatalf("config with a template block should still load: %v", err)
	}
	if cfg.Base == nil || cfg.Base.PAT != "old_secret" {
		t.Fatalf("template.pat should migrate to base.pat: %+v", cfg.Base)
	}
	r, err := cfg.Resolve("local")
	if err != nil {
		t.Fatal(err)
	}
	if r.Base.URL != DefaultBaseURL || r.Base.Branch != DefaultBaseBranch {
		t.Fatalf("template url/branch must be dropped for the base defaults: %+v", r.Base)
	}
	if r.Base.PAT != "old_secret" {
		t.Fatalf("resolved base pat = %q", r.Base.PAT)
	}
}
