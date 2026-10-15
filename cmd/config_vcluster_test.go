package cmd

import (
	"os"
	"strings"
	"testing"

	"github.com/micronisus/leptostack-cli/internal/config"
	"github.com/micronisus/leptostack-cli/internal/testutil"
)

func TestSetConfigValueVClusterFeatures(t *testing.T) {
	cfg := &config.Config{Clusters: map[string]*config.Cluster{
		"dev": {Provider: config.ProviderVCluster, VCluster: &config.VCluster{HostContext: "ctx"}},
	}}
	if err := setConfigValueIn(cfg, "", "clusters.dev.vcluster.metricsServer", "false", false); err != nil {
		t.Fatal(err)
	}
	if err := setConfigValueIn(cfg, "", "clusters.dev.vcluster.oidc", "off", false); err != nil {
		t.Fatal(err)
	}
	if cfg.Clusters["dev"].VCluster.MetricsEnabled() {
		t.Error("metricsServer should be disabled")
	}
	if cfg.Clusters["dev"].VCluster.OIDCEnabled() {
		t.Error("oidc should be disabled")
	}
	if err := setConfigValueIn(cfg, "", "clusters.dev.vcluster.oidc", "maybe", false); err == nil {
		t.Error("expected an error for a non-boolean value")
	}
	if err := setConfigValueIn(cfg, "", "clusters.dev.vcluster.apiServerServiceType", "clusterip", false); err != nil {
		t.Fatal(err)
	}
	if got := cfg.Clusters["dev"].VCluster.APIServerServiceType; got != config.VClusterServiceTypeClusterIP {
		t.Errorf("apiServerServiceType = %q, want ClusterIP", got)
	}
	if err := setConfigValueIn(cfg, "", "clusters.dev.vcluster.apiServerServiceType", "nope", false); err == nil {
		t.Error("expected an error for an invalid service type")
	}
}

func TestConfigInitDisablesVClusterFeatures(t *testing.T) {
	env := newEnv(t)
	env.Tool(t, "vcluster", "exit 0\n")
	cfgPath := testutil.SetHomeConfig(t, "")

	_, errOut, code := runCLI(t, "",
		"config", "init", "--non-interactive", "--provider", "vcluster",
		"--cluster-name", "dev", "--domain", "alice.test",
		"--git-server", "github", "--host-context", "che-ctx",
		"--disable-vcluster-metrics", "--disable-vcluster-oidc",
		"--vcluster-api-server-service-type", "clusterip",
	)
	if code != 0 {
		t.Fatalf("exit code = %d (stderr=%s)", code, errOut)
	}
	b, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	cfg := string(b)
	for _, want := range []string{"metricsServer: false", "oidc: false", "apiServerServiceType: ClusterIP"} {
		if !strings.Contains(cfg, want) {
			t.Errorf("config missing %q:\n%s", want, cfg)
		}
	}
}

func TestConfigInitDefaultsVClusterFeaturesEnabled(t *testing.T) {
	env := newEnv(t)
	env.Tool(t, "vcluster", "exit 0\n")
	cfgPath := testutil.SetHomeConfig(t, "")

	_, errOut, code := runCLI(t, "",
		"config", "init", "--non-interactive", "--provider", "vcluster",
		"--cluster-name", "dev", "--domain", "alice.test",
		"--git-server", "github", "--host-context", "che-ctx",
	)
	if code != 0 {
		t.Fatalf("exit code = %d (stderr=%s)", code, errOut)
	}
	b, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	cfg := string(b)
	if strings.Contains(cfg, "metricsServer") || strings.Contains(cfg, "oidc:") || strings.Contains(cfg, "apiServerServiceType") {
		t.Errorf("feature toggles must be omitted when left at their defaults:\n%s", cfg)
	}
}
