package gitops

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/micronisus/leptostack-cli/internal/config"
	"github.com/micronisus/leptostack-cli/internal/execx"
	"github.com/micronisus/leptostack-cli/internal/kubernetes"
)

// renameExampleReferences rewrites the template's example cluster references.
func renameExampleReferences(path, clusterName string) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	s := string(b)
	s = strings.ReplaceAll(s, "example-keycloak", clusterName+"-keycloak")
	s = strings.ReplaceAll(s, "leptostack-example", "leptostack-"+clusterName)
	s = strings.ReplaceAll(s, "leptostack-module-example", "leptostack-module-"+clusterName)
	s = strings.ReplaceAll(s, "overlays/example", "overlays/"+clusterName)
	return os.WriteFile(path, []byte(s), 0o644)
}

// internalCADeletePatch is the /spec/patches entry that deletes the template's
// self-signed internal-ca Certificate so cert-manager stops (re)generating the
// internal CA — the CLI provides internal-ca-secret instead.
var internalCADeletePatch = []string{
	"          - target:",
	"              group: cert-manager.io",
	"              version: v1",
	"              kind: Certificate",
	"              name: internal-ca",
	"              namespace: cert-manager",
	"            patch: |",
	"              $patch: delete",
	"              apiVersion: cert-manager.io/v1",
	"              kind: Certificate",
	"              metadata:",
	"                name: internal-ca",
	"                namespace: cert-manager",
}

// addInternalCADeletePatch inserts a patch deleting the self-signed internal-ca
// Certificate into the infra-config Kustomization's /spec/patches value (the
// Kustomization that owns the Certificate). It returns whether it changed the
// file.
func addInternalCADeletePatch(path string) (bool, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return false, err
	}
	content := string(b)
	if hasInternalCADelete(content) {
		return false, nil
	}

	lines := strings.Split(content, "\n")
	out := make([]string, 0, len(lines)+len(internalCADeletePatch))
	inserted := false
	inInfraConfig := false
	armed := false
	for _, line := range lines {
		out = append(out, line)
		trimmed := strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(trimmed, "- target:"):
			inInfraConfig = false
		case trimmed == "name: infra-config":
			inInfraConfig = true
		}
		if !inInfraConfig || inserted {
			continue
		}
		if !armed && strings.HasPrefix(trimmed, "path:") && strings.Contains(trimmed, "/spec/patches") {
			armed = true
			continue
		}
		if armed && trimmed == "value:" {
			out = append(out, internalCADeletePatch...)
			inserted = true
			armed = false
		}
	}
	if !inserted {
		return false, nil
	}
	return true, os.WriteFile(path, []byte(strings.Join(out, "\n")), 0o644)
}

// hasInternalCADelete reports whether the delete patch is already present. It
// matches the Certificate name exactly so the template's "name: internal-ca-bundle"
// Bundle patch is not mistaken for it.
func hasInternalCADelete(content string) bool {
	for _, line := range strings.Split(content, "\n") {
		if strings.TrimSpace(line) == "name: internal-ca" {
			return true
		}
	}
	return false
}

// generateAppOverlay copies apps/<group>/overlays/example to
// apps/<group>/overlays/<clusterName> when the template provides it and sets the
// cluster-specific namePrefix. Groups without an example overlay are skipped.
func (m *Manager) generateAppOverlay(repoDir, group, clusterName string) error {
	exampleOverlay := filepath.Join(repoDir, "apps", group, "overlays", "example")
	if !fileExists(filepath.Join(exampleOverlay, "kustomization.yaml")) {
		return nil
	}

	overlayDir := filepath.Join(repoDir, "apps", group, "overlays", clusterName)
	m.IO.Errf("Creating cluster overlay at apps/%s/overlays/%s from the embedded template's example overlay...\n", group, clusterName)
	if err := os.MkdirAll(overlayDir, 0o755); err != nil {
		return err
	}
	for _, f := range []string{"naming-conf.yaml", "kustomization.yaml"} {
		src := filepath.Join(exampleOverlay, f)
		if !fileExists(src) {
			continue
		}
		if err := copyPath(src, filepath.Join(overlayDir, f)); err != nil {
			return err
		}
	}
	if fileExists(filepath.Join(exampleOverlay, "module-core-images.yaml")) {
		dst := filepath.Join(overlayDir, "module-core-images.yaml")
		if err := copyPath(filepath.Join(exampleOverlay, "module-core-images.yaml"), dst); err != nil {
			return err
		}
		if err := replaceInFile(dst, "example-", clusterName+"-"); err != nil {
			return err
		}
		m.IO.Errf("  Copied module-core-images.yaml and rewrote example- references to %s-.\n", clusterName)
	}

	return replaceLinePrefix(filepath.Join(overlayDir, "kustomization.yaml"), "namePrefix:", "namePrefix: "+clusterName+"-")
}

func (m *Manager) generateClusterConfig(ctx context.Context, repoDir string) error {
	r := m.Resolved
	clusterName := r.ClusterName
	domain := r.Domain
	infraDir := filepath.Join(repoDir, "infrastructure")
	infraKust := filepath.Join(infraDir, "kustomization.yaml")
	clusterConfigFile := filepath.Join(infraDir, "cluster-config.yaml")

	if !fileExists(infraKust) {
		return fmt.Errorf("could not find infrastructure/kustomization.yaml in the repository")
	}

	// The registry ingress/route is only required for minikube. A vcluster
	// reaches the registry through the host, so no cluster-config.yaml is
	// created and nothing is added to infrastructure/kustomization.yaml.
	lbIP := ""
	if r.Provider == config.ProviderMinikube {
		m.IO.Errln("Creating infrastructure/cluster-config.yaml...")
		minikubeIP := m.minikubeIP(ctx)
		lbIP = kubernetes.Subnet100(minikubeIP)
		content := minikubeClusterConfig(clusterName, domain, minikubeIP)
		if err := os.WriteFile(clusterConfigFile, []byte(content), 0o644); err != nil {
			return err
		}

		infraContent, err := os.ReadFile(infraKust)
		if err != nil {
			return err
		}
		if strings.Contains(string(infraContent), "cluster-config.yaml") {
			m.IO.Errln("  cluster-config.yaml already referenced in infrastructure/kustomization.yaml.")
		} else {
			m.IO.Errln("  Adding cluster-config.yaml to infrastructure/kustomization.yaml resources...")
			if err := insertAfterMarker(infraKust, "resources:", "  - cluster-config.yaml"); err != nil {
				return err
			}
		}
	} else {
		m.IO.Errln("Skipping infrastructure/cluster-config.yaml (registry ingress is not required for vcluster).")
	}

	for _, group := range AppGroups {
		if err := m.generateAppOverlay(repoDir, group, clusterName); err != nil {
			return err
		}
	}

	overlayKust := filepath.Join(repoDir, "apps", "leptostack", "overlays", clusterName, "kustomization.yaml")
	if !fileExists(overlayKust) {
		m.IO.Errln("Skipping the leptostack overlay patches (no example overlay in the embedded template).")
		return nil
	}

	overlayContent, err := os.ReadFile(overlayKust)
	if err != nil {
		return err
	}
	if !strings.Contains(string(overlayContent), "greenmail.yaml") {
		if err := insertAfterMarker(overlayKust, "resources:", "  - ../../base/greenmail.yaml"); err != nil {
			return err
		}
		m.IO.Errln("  Added ../../base/greenmail.yaml to overlay kustomization resources.")
	}

	if !strings.Contains(string(overlayContent), "name: apisix") {
		block := apisixPatch(r.Provider != config.ProviderVCluster, lbIP)
		if err := appendString(overlayKust, block); err != nil {
			return err
		}
		m.IO.Errln("  Added apisix LoadBalancer patch to overlay kustomization.")
	}

	if !strings.Contains(string(overlayContent), "name: leptostack-config") {
		block := fmt.Sprintf(`
  - target:
      kind: ConfigMap
      name: leptostack-config
      namespace: flux-system
    patch: |-
      - op: replace
        path: /data/cluster_name
        value: %s
      - op: replace
        path: /data/leptostack_domain
        value: %s
      - op: replace
        path: /data/leptostack_name
        value: %s
`, clusterName, r.Domain, r.LeptostackName)
		if err := appendString(overlayKust, block); err != nil {
			return err
		}
		m.IO.Errln("  Added leptostack-config patch (cluster_name, leptostack_domain, leptostack_name) to overlay kustomization.")
	}

	return nil
}

func (m *Manager) minikubeIP(ctx context.Context) string {
	out, err := m.R.Output(ctx, execx.Cmd{Name: "minikube", Args: []string{"ip", "-p", m.Resolved.MinikubeProfile}})
	if err != nil {
		return ""
	}
	return strings.TrimSpace(out)
}

func apisixPatch(withLoadBalancerIP bool, lbIP string) string {
	var b strings.Builder
	b.WriteString(`
  - target:
      kind: Kustomization
      name: apisix
    patch: |-
      - op: add
        path: /spec/patches
        value:
          - target:
              kind: HelmRelease
              name: apisix
            patch: |-
              - op: add
                path: /spec/values/service
                value:
                  type: LoadBalancer
`)
	if withLoadBalancerIP && lbIP != "" {
		fmt.Fprintf(&b, "                  loadBalancerIP: %s\n", lbIP)
	}
	return b.String()
}
