package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/micronisus/leptostack-cli/internal/config"
	"github.com/micronisus/leptostack-cli/internal/testutil"
)

func TestSetConfigValueScopesSharedSectionsToSelectedCluster(t *testing.T) {
	cfg := &config.Config{
		CurrentContext: "local",
		Clusters: map[string]*config.Cluster{
			"local": {Provider: config.ProviderMinikube, Base: &config.Base{PAT: "old"}},
		},
	}
	if err := setConfigValueIn(cfg, "local", "base.pat", "new", true); err != nil {
		t.Fatal(err)
	}
	if got := cfg.Clusters["local"].Base.PAT; got != "new" {
		t.Errorf("clusters.local.base.pat = %q, want new", got)
	}
}

func TestSetConfigValueTargetsCurrentContextWithoutFlag(t *testing.T) {
	cfg := &config.Config{
		CurrentContext: "local",
		Clusters: map[string]*config.Cluster{
			"local": {Provider: config.ProviderMinikube, Base: &config.Base{PAT: "old"}},
		},
	}
	if err := setConfigValueIn(cfg, "", "base.pat", "new", true); err != nil {
		t.Fatal(err)
	}
	if got := cfg.Clusters["local"].Base.PAT; got != "new" {
		t.Errorf("clusters.local.base.pat = %q, want new", got)
	}
}

func TestSetConfigValueExplicitClusterKey(t *testing.T) {
	cfg := &config.Config{Clusters: map[string]*config.Cluster{}}
	if err := setConfigValueIn(cfg, "", "clusters.dev.git.branch", "dev", false); err != nil {
		t.Fatal(err)
	}
	cl := cfg.Clusters["dev"]
	if cl == nil || cl.Git == nil || cl.Git.Branch != "dev" {
		t.Fatalf("clusters.dev.git.branch not set: %+v", cl)
	}
	if err := setConfigValueIn(cfg, "", "clusters.dev.git.caFile", "/etc/pki/ca.pem", false); err != nil {
		t.Fatal(err)
	}
	if cl.Git.CAFile != "/etc/pki/ca.pem" {
		t.Errorf("clusters.dev.git.caFile = %q, want /etc/pki/ca.pem", cl.Git.CAFile)
	}
	fluxCA := filepath.Join(t.TempDir(), "flux-ca.pem")
	if err := os.WriteFile(fluxCA, []byte("CA"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := setConfigValueIn(cfg, "", "clusters.dev.flux.caFile", fluxCA, false); err != nil {
		t.Fatal(err)
	}
	if cl.Flux == nil || cl.Flux.CAFile != fluxCA {
		t.Errorf("clusters.dev.flux.caFile = %+v, want %q", cl.Flux, fluxCA)
	}
	if err := setConfigValueIn(cfg, "", "clusters.dev.flux.caFile", "/does/not/exist.pem", false); err == nil {
		t.Error("expected an error for an unreadable flux CA file")
	}
	if err := setConfigValueIn(cfg, "", "clusters.dev.git.server", "nope", false); err == nil {
		t.Error("expected an error for an invalid git server")
	}
	if err := setConfigValueIn(cfg, "", "clusters.dev.registry.credentials", "nocolon", false); err == nil {
		t.Error("expected an error for registry credentials without a colon")
	}
}

func TestSetConfigValueClusterSecretGuard(t *testing.T) {
	cfg := &config.Config{Clusters: map[string]*config.Cluster{"local": {Provider: config.ProviderMinikube}}}
	if err := setConfigValueIn(cfg, "local", "base.pat", "ghp_secret", false); err == nil {
		t.Error("expected the secret guard to reject a PAT without --secret")
	}
	if got := cfg.Clusters["local"].Base; got != nil {
		t.Errorf("base must not be created when rejected: %+v", got)
	}
}

func TestUnsetConfigValueScopesToSelectedCluster(t *testing.T) {
	cfg := &config.Config{
		Clusters: map[string]*config.Cluster{
			"local": {Provider: config.ProviderMinikube, Base: &config.Base{PAT: "keep"}},
		},
	}
	if err := unsetConfigValueIn(cfg, "local", "base.pat"); err != nil {
		t.Fatal(err)
	}
	if got := cfg.Clusters["local"].Base.PAT; got != "" {
		t.Errorf("clusters.local.base.pat = %q, want empty", got)
	}
	if err := unsetConfigValueIn(cfg, "local", "bogus"); err == nil {
		t.Error("expected an error for an unknown key")
	}
}

func TestUnsetConfigValueTargetsCurrentContext(t *testing.T) {
	cfg := &config.Config{
		CurrentContext: "local",
		Clusters: map[string]*config.Cluster{
			"local": {Provider: config.ProviderMinikube, Git: &config.Git{PAT: "y"}},
		},
	}
	if err := unsetConfigValueIn(cfg, "", "git.pat"); err != nil {
		t.Fatal(err)
	}
	if got := cfg.Clusters["local"].Git.PAT; got != "" {
		t.Errorf("clusters.local.git.pat = %q, want empty", got)
	}
}

func TestSetConfigValueRejectsUnknownSelectedCluster(t *testing.T) {
	cfg := &config.Config{
		CurrentContext: "local",
		Clusters:       map[string]*config.Cluster{"local": {Provider: config.ProviderMinikube}},
	}
	if err := setConfigValueIn(cfg, "typo", "base.pat", "x", true); err == nil {
		t.Error("expected an error for an unknown selected cluster")
	}
	if _, ok := cfg.Clusters["typo"]; ok {
		t.Error("an unknown selected cluster must not be created")
	}
}

func TestSetConfigValueRequiresUnambiguousCluster(t *testing.T) {
	cfg := &config.Config{
		Clusters: map[string]*config.Cluster{
			"a": {Provider: config.ProviderMinikube},
			"b": {Provider: config.ProviderMinikube},
		},
	}
	if err := setConfigValueIn(cfg, "", "base.pat", "x", true); err == nil {
		t.Error("expected an error when no cluster can be unambiguously selected")
	}
	if _, ok := cfg.Clusters["local"]; ok {
		t.Error("no phantom cluster may be created")
	}
}

func TestConfigSetClusterScopedKeyE2E(t *testing.T) {
	newEnv(t)
	cfgPath := testutil.SetHomeConfig(t, `apiVersion: v1
kind: LeptostackConfig
current-context: local
clusters:
  local:
    provider: minikube
    clusterName: local
    base:
      pat: old
`)
	_, errOut, code := runCLI(t, "", "-c", "local", "config", "set", "base.pat", "yyy", "--secret")
	if code != 0 {
		t.Fatalf("exit code = %d (stderr=%s)", code, errOut)
	}
	b, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), "\nbase:") {
		t.Errorf("a root-level base was written:\n%s", b)
	}
	cfg, err := config.LoadFrom(cfgPath)
	if err != nil {
		t.Fatalf("reload config: %v\n%s", err, b)
	}
	if got := cfg.Clusters["local"].Base.PAT; got != "yyy" {
		t.Errorf("clusters.local.base.pat = %q, want yyy\n%s", got, b)
	}
}
