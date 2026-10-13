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
	if err := setConfigValue(cfg, "clusters.dev.vcluster.metricsServer", "false", false); err != nil {
		t.Fatal(err)
	}
	if err := setConfigValue(cfg, "clusters.dev.vcluster.oidc", "off", false); err != nil {
		t.Fatal(err)
	}
	if cfg.Clusters["dev"].VCluster.MetricsEnabled() {
		t.Error("metricsServer should be disabled")
	}
	if cfg.Clusters["dev"].VCluster.OIDCEnabled() {
		t.Error("oidc should be disabled")
	}
	if err := setConfigValue(cfg, "clusters.dev.vcluster.oidc", "maybe", false); err == nil {
		t.Error("expected an error for a non-boolean value")
	}
}

func TestConfigInitDisablesVClusterFeatures(t *testing.T) {
	env := newEnv(t)
	env.Tool(t, "vcluster", "exit 0\n")
	cfgPath := testutil.SetHomeConfig(t, "")

	_, errOut, code := runCLI(t, "",
		"config", "init", "--non-interactive", "--provider", "vcluster",
		"--cluster", "dev", "--cluster-name", "dev", "--domain", "alice.test",
		"--git-server", "github", "--host-context", "che-ctx",
		"--disable-vcluster-metrics", "--disable-vcluster-oidc",
	)
	if code != 0 {
		t.Fatalf("exit code = %d (stderr=%s)", code, errOut)
	}
	b, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	cfg := string(b)
	for _, want := range []string{"metricsServer: false", "oidc: false"} {
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
		"--cluster", "dev", "--cluster-name", "dev", "--domain", "alice.test",
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
	if strings.Contains(cfg, "metricsServer") || strings.Contains(cfg, "oidc:") {
		t.Errorf("feature toggles must be omitted when left at their defaults:\n%s", cfg)
	}
}
