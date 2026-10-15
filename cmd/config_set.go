package cmd

import (
	"strconv"
	"strings"

	"github.com/micronisus/leptostack-cli/internal/cli"
	"github.com/micronisus/leptostack-cli/internal/config"
)

func atoiOr(s string, def int) int {
	n, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil || n <= 0 {
		return def
	}
	return n
}

// boolPtr returns a pointer to b, for optional boolean configuration fields.
func boolPtr(b bool) *bool { return &b }

// parseConfigBool parses a user-supplied boolean configuration value.
func parseConfigBool(value string) (bool, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "true", "1", "yes", "on":
		return true, nil
	case "false", "0", "no", "off":
		return false, nil
	}
	return false, cli.Exitf(cli.ExitUsage, "expected a boolean value (true or false), got %q", value)
}

// parseVClusterServiceType normalizes a vcluster API server Service type,
// accepting the case-insensitive forms loadbalancer and clusterip.
func parseVClusterServiceType(value string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "loadbalancer":
		return config.VClusterServiceTypeLoadBalancer, nil
	case "clusterip":
		return config.VClusterServiceTypeClusterIP, nil
	}
	return "", cli.Exitf(cli.ExitUsage, "expected a service type (loadbalancer or clusterip), got %q", value)
}

// scopedSections are the cluster sections that may also be written with their
// short, unqualified key (for example `base.pat`), targeting the selected
// cluster. They are cluster-scoped only; there are no global sections.
var scopedSections = []string{"git.", "registry.", "base."}

// isScopedSectionKey reports whether key addresses a cluster-scoped section.
func isScopedSectionKey(key string) bool {
	for _, prefix := range scopedSections {
		if strings.HasPrefix(key, prefix) {
			return true
		}
	}
	return false
}

// resolveScopeTarget returns the cluster a short (unqualified) shared-section
// key should target: the explicitly selected cluster, otherwise the current
// cluster. It returns "" with no error when the key needs no scoping, and fails
// when no existing cluster can be unambiguously selected.
func resolveScopeTarget(cfg *config.Config, cluster, key string) (string, error) {
	if strings.HasPrefix(key, "clusters.") || !isScopedSectionKey(key) {
		return "", nil
	}
	if cluster != "" {
		if _, ok := cfg.Clusters[cluster]; !ok {
			return "", cli.Exitf(cli.ExitUsage, "unknown cluster %q", cluster)
		}
		return cluster, nil
	}
	if cfg.CurrentContext != "" {
		if _, ok := cfg.Clusters[cfg.CurrentContext]; !ok {
			return "", cli.Exitf(cli.ExitUsage, "current-context cluster %q does not exist", cfg.CurrentContext)
		}
		return cfg.CurrentContext, nil
	}
	if len(cfg.Clusters) == 1 {
		for name := range cfg.Clusters {
			return name, nil
		}
	}
	return "", cli.Exitf(cli.ExitUsage, "no cluster selected; use -c/--cluster or 'config use-context'")
}

// scopeKey rewrites a short shared-section key (git.*, registry.*, base.*) so it
// targets the given cluster. Keys that already name a cluster, or that are not
// cluster-scoped, are returned unchanged.
func scopeKey(cluster, key string) string {
	if cluster == "" || strings.HasPrefix(key, "clusters.") || !isScopedSectionKey(key) {
		return key
	}
	return "clusters." + cluster + "." + key
}

// setConfigValueIn sets a dotted configuration key. When cluster is empty the
// current cluster is used; the shared sections are always cluster-scoped.
func setConfigValueIn(cfg *config.Config, cluster, key, value string, secret bool) error {
	key = strings.TrimSpace(key)
	lower := strings.ToLower(value)
	if !secret && looksSecret(key, lower) {
		return cli.Exitf(cli.ExitUsage, "refusing to set %q to a secret-looking value without --secret", key)
	}
	target, err := resolveScopeTarget(cfg, cluster, key)
	if err != nil {
		return err
	}
	key = scopeKey(target, key)

	switch key {
	case "current-context":
		return setCurrentContext(cfg, value)
	case "preferences.autoUpgradeCheck":
		b := value == "true"
		if cfg.Preferences == nil {
			cfg.Preferences = &config.Preferences{}
		}
		cfg.Preferences.AutoUpgradeCheck = &b
	default:
		return setClusterValue(cfg, key, value)
	}
	return nil
}

func setClusterValue(cfg *config.Config, key, value string) error {
	if !strings.HasPrefix(key, "clusters.") {
		return cli.Exitf(cli.ExitUsage, "unknown configuration key %q", key)
	}
	rest := strings.TrimPrefix(key, "clusters.")
	dot := strings.IndexByte(rest, '.')
	if dot < 0 {
		return cli.Exitf(cli.ExitUsage, "unknown configuration key %q", key)
	}
	name := rest[:dot]
	field := rest[dot+1:]
	cl := getOrCreateCluster(cfg, name, "", "")
	switch field {
	case "provider":
		if err := validateProvider(value); err != nil {
			return err
		}
		cl.Provider = value
	case "clusterName":
		if err := config.ValidateClusterName(value); err != nil {
			return cli.MapConfigError(err)
		}
		cl.ClusterName = value
	case "leptostackName":
		cl.LeptostackName = value
	case "domain":
		if err := config.ValidateDomain(value); err != nil {
			return cli.MapConfigError(err)
		}
		cl.Domain = value
	case "vcluster.hostContext":
		ensureVCluster(cl).HostContext = value
	case "vcluster.hostNamespace":
		ensureVCluster(cl).HostNamespace = value
	case "vcluster.metricsServer":
		b, err := parseConfigBool(value)
		if err != nil {
			return err
		}
		ensureVCluster(cl).Metrics = &b
	case "vcluster.oidc":
		b, err := parseConfigBool(value)
		if err != nil {
			return err
		}
		ensureVCluster(cl).OIDC = &b
	case "vcluster.apiServerServiceType":
		t, err := parseVClusterServiceType(value)
		if err != nil {
			return err
		}
		ensureVCluster(cl).APIServerServiceType = t
	case "minikube.cpus":
		ensureMinikube(cl).CPUs = atoiOr(value, config.DefaultMinikubeCPUs)
	case "minikube.memoryMB":
		ensureMinikube(cl).MemoryMB = atoiOr(value, config.DefaultMinikubeMemory)
	case "git.server":
		if err := validateGitServer(value); err != nil {
			return err
		}
		ensureClusterGit(cl).Server = value
	case "git.hostname":
		ensureClusterGit(cl).Hostname = value
	case "git.owner":
		ensureClusterGit(cl).Owner = value
	case "git.repository":
		ensureClusterGit(cl).Repository = value
	case "git.branch":
		ensureClusterGit(cl).Branch = value
	case "git.clusterPath":
		ensureClusterGit(cl).ClusterPath = value
	case "git.pat":
		ensureClusterGit(cl).PAT = value
	case "git.caFile":
		ensureClusterGit(cl).CAFile = value
	case "registry.repository":
		ensureClusterRegistry(cl).Repository = value
	case "registry.credentials":
		if !strings.Contains(value, ":") {
			return cli.Exitf(cli.ExitUsage, "registry credentials must be in user:pass format")
		}
		ensureClusterRegistry(cl).Credentials = value
	case "base.url":
		ensureClusterBase(cl).URL = value
	case "base.branch":
		ensureClusterBase(cl).Branch = value
	case "base.pat":
		ensureClusterBase(cl).PAT = value
	default:
		return cli.Exitf(cli.ExitUsage, "unknown configuration key %q", key)
	}
	return nil
}

// unsetConfigValueIn clears a dotted configuration key. When cluster is empty
// the current cluster is used; the shared sections are always cluster-scoped.
func unsetConfigValueIn(cfg *config.Config, cluster, key string) error {
	key = strings.TrimSpace(key)
	target, err := resolveScopeTarget(cfg, cluster, key)
	if err != nil {
		return err
	}
	key = scopeKey(target, key)
	switch key {
	case "current-context":
		cfg.CurrentContext = ""
	default:
		return unsetClusterValue(cfg, key)
	}
	return nil
}

// unsetClusterValue clears a per-cluster value. Only secret fields are
// unsettable, matching the global behavior.
func unsetClusterValue(cfg *config.Config, key string) error {
	if !strings.HasPrefix(key, "clusters.") {
		return cli.Exitf(cli.ExitUsage, "unknown or non-unsettable configuration key %q", key)
	}
	rest := strings.TrimPrefix(key, "clusters.")
	dot := strings.IndexByte(rest, '.')
	if dot < 0 {
		return cli.Exitf(cli.ExitUsage, "unknown or non-unsettable configuration key %q", key)
	}
	cl := cfg.Clusters[rest[:dot]]
	if cl == nil {
		return cli.Exitf(cli.ExitUsage, "unknown or non-unsettable configuration key %q", key)
	}
	switch rest[dot+1:] {
	case "git.pat":
		if cl.Git != nil {
			cl.Git.PAT = ""
		}
	case "registry.credentials":
		if cl.Registry != nil {
			cl.Registry.Credentials = ""
		}
	case "base.pat":
		if cl.Base != nil {
			cl.Base.PAT = ""
		}
	default:
		return cli.Exitf(cli.ExitUsage, "unknown or non-unsettable configuration key %q", key)
	}
	return nil
}

func setCurrentContext(cfg *config.Config, value string) error {
	if _, ok := cfg.Clusters[value]; !ok {
		return cli.Exitf(cli.ExitConfig, "unknown cluster %q", value)
	}
	cfg.CurrentContext = value
	return nil
}

// getOrCreateCluster returns the named cluster entry, creating a minimal one
// when absent so callers converge on the same initialization. clusterName and
// provider are only applied when creating; an empty clusterName defaults to the
// entry key.
func getOrCreateCluster(cfg *config.Config, name, clusterName, provider string) *config.Cluster {
	if cl, ok := cfg.Clusters[name]; ok {
		return cl
	}
	if clusterName == "" {
		clusterName = name
	}
	cl := &config.Cluster{ClusterName: clusterName, Provider: provider}
	cfg.Clusters[name] = cl
	return cl
}

func ensureClusterGit(cl *config.Cluster) *config.Git {
	if cl.Git == nil {
		cl.Git = &config.Git{}
	}
	return cl.Git
}

func ensureClusterRegistry(cl *config.Cluster) *config.Registry {
	if cl.Registry == nil {
		cl.Registry = &config.Registry{}
	}
	return cl.Registry
}

func ensureClusterBase(cl *config.Cluster) *config.Base {
	if cl.Base == nil {
		cl.Base = &config.Base{}
	}
	return cl.Base
}

func ensureVCluster(cl *config.Cluster) *config.VCluster {
	if cl.VCluster == nil {
		cl.VCluster = &config.VCluster{}
	}
	return cl.VCluster
}

func ensureMinikube(cl *config.Cluster) *config.Minikube {
	if cl.Minikube == nil {
		cl.Minikube = &config.Minikube{}
	}
	return cl.Minikube
}

// looksSecret reports whether a key/value pair is likely a credential.
func looksSecret(key, lowerValue string) bool {
	lk := strings.ToLower(key)
	if strings.Contains(lk, "pat") || strings.Contains(lk, "password") ||
		strings.Contains(lk, "credential") || strings.Contains(lk, "token") ||
		strings.Contains(lk, "secret") {
		return true
	}
	return strings.HasPrefix(lowerValue, "ghp_") || strings.HasPrefix(lowerValue, "glpat-")
}
