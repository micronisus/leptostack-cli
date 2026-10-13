package providers

import (
	"strings"
	"testing"

	"github.com/micronisus/leptostack-cli/internal/config"
	"sigs.k8s.io/yaml"
)

func boolp(b bool) *bool { return &b }

func renderValuesFor(t *testing.T, metrics, oidc *bool) map[string]any {
	t.Helper()
	values := renderVClusterValues(&config.Resolved{
		ClusterName:    "dev-alice",
		LeptostackName: "portal",
		Domain:         "alice-leptostack.test",
		VCluster:       config.VCluster{Metrics: metrics, OIDC: oidc},
	})
	if strings.Contains(values, "__") {
		t.Fatalf("unsubstituted placeholder remains:\n%s", values)
	}
	var doc map[string]any
	if err := yaml.Unmarshal([]byte(values), &doc); err != nil {
		t.Fatalf("rendered values are not valid YAML: %v\n%s", err, values)
	}
	return doc
}

func dig(t *testing.T, doc map[string]any, path ...string) any {
	t.Helper()
	var cur any = doc
	for i, p := range path {
		m, ok := cur.(map[string]any)
		if !ok {
			t.Fatalf("path %v: %q is not a map", path, path[i-1])
		}
		v, ok := m[p]
		if !ok {
			t.Fatalf("path %v: missing key %q", path, p)
		}
		cur = v
	}
	return cur
}

func TestVClusterValuesEnableFeaturesByDefault(t *testing.T) {
	doc := renderValuesFor(t, nil, nil)
	if got := dig(t, doc, "integrations", "metricsServer", "enabled"); got != true {
		t.Errorf("metricsServer.enabled = %v, want true", got)
	}
	args, ok := dig(t, doc, "controlPlane", "distro", "k8s", "apiServer", "extraArgs").([]any)
	if !ok {
		t.Fatalf("apiServer.extraArgs is not a list")
	}
	if len(args) != 5 {
		t.Errorf("expected 5 OIDC extraArgs by default, got %d: %v", len(args), args)
	}
	if first, _ := args[0].(string); first != "--oidc-issuer-url=https://portal-sso.alice-leptostack.test/realms/dev-alice" {
		t.Errorf("oidc issuer arg = %q", first)
	}
	if _, ok := dig(t, doc, "controlPlane", "statefulSet", "persistence").(map[string]any); !ok {
		t.Error("expected the oidc-ca persistence block by default")
	}
	corefile, _ := dig(t, doc, "controlPlane", "coredns", "overwriteConfig").(string)
	for _, want := range []string{
		".:1053 {",
		"prometheus :9153",
		"apisix-internal-gateway.ingress-dev-alice-internal.svc.cluster.local",
		"apisix-gateway.ingress-dev-alice.svc.cluster.local",
		"import /etc/coredns/custom/*.server",
	} {
		if !strings.Contains(corefile, want) {
			t.Errorf("coredns overwriteConfig missing %q:\n%s", want, corefile)
		}
	}
}

func TestVClusterValuesDisableMetricsAndOIDC(t *testing.T) {
	off := boolp(false)
	doc := renderValuesFor(t, off, off)
	if got := dig(t, doc, "integrations", "metricsServer", "enabled"); got != false {
		t.Errorf("metricsServer.enabled = %v, want false", got)
	}
	args, ok := dig(t, doc, "controlPlane", "distro", "k8s", "apiServer", "extraArgs").([]any)
	if !ok || len(args) != 0 {
		t.Errorf("expected no OIDC extraArgs when disabled, got %v", args)
	}
	ss, ok := dig(t, doc, "controlPlane", "statefulSet").(map[string]any)
	if !ok {
		t.Fatal("statefulSet is not a map")
	}
	if _, present := ss["persistence"]; present {
		t.Error("persistence block must be omitted when OIDC is disabled")
	}
	if dig(t, doc, "controlPlane", "coredns", "overwriteConfig") == nil {
		t.Error("coredns config must remain when the features are disabled")
	}
	if dig(t, doc, "rbac", "clusterRole", "enabled") != false {
		t.Error("rbac.clusterRole.enabled must remain false")
	}
}

func TestVClusterValuesOmitOIDCArtifactsWhenDisabled(t *testing.T) {
	off := boolp(false)
	values := renderVClusterValues(&config.Resolved{
		ClusterName:    "dev-alice",
		LeptostackName: "portal",
		Domain:         "alice-leptostack.test",
		VCluster:       config.VCluster{OIDC: off},
	})
	for _, unwanted := range []string{"oidc-ca", "--oidc", "/etc/oidc"} {
		if strings.Contains(values, unwanted) {
			t.Errorf("OIDC artifact %q must be absent when OIDC is disabled:\n%s", unwanted, values)
		}
	}
}
