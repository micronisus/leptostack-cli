package config

import (
	"strings"
	"testing"

	"sigs.k8s.io/yaml"
)

func TestVClusterFeatureFlagsDefaultEnabled(t *testing.T) {
	var v VCluster
	if !v.MetricsEnabled() {
		t.Error("metrics must default to enabled")
	}
	if !v.OIDCEnabled() {
		t.Error("OIDC must default to enabled")
	}

	off := false
	v.Metrics = &off
	v.OIDC = &off
	if v.MetricsEnabled() {
		t.Error("metrics must be disabled when set false")
	}
	if v.OIDCEnabled() {
		t.Error("OIDC must be disabled when set false")
	}
}

func TestVClusterFeatureFlagsYAMLRoundTrip(t *testing.T) {
	off, on := false, true
	in := &Cluster{
		Provider: ProviderVCluster,
		VCluster: &VCluster{HostContext: "ctx", HostNamespace: "ns", Metrics: &off, OIDC: &on},
	}
	b, err := yaml.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"metricsServer: false", "oidc: true"} {
		if !strings.Contains(string(b), want) {
			t.Errorf("marshalled config missing %q:\n%s", want, b)
		}
	}
	var out Cluster
	if err := yaml.UnmarshalStrict(b, &out); err != nil {
		t.Fatalf("strict unmarshal: %v", err)
	}
	if out.VCluster.MetricsEnabled() {
		t.Error("metrics should round-trip as disabled")
	}
	if !out.VCluster.OIDCEnabled() {
		t.Error("OIDC should round-trip as enabled")
	}
}
