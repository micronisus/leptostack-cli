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

// setConfigValue sets a dotted configuration key with enum/format validation.
func setConfigValue(cfg *config.Config, key, value string, secret bool) error {
	key = strings.TrimSpace(key)
	lower := strings.ToLower(value)
	if !secret && looksSecret(key, lower) {
		return cli.Exitf(cli.ExitUsage, "refusing to set %q to a secret-looking value without --secret", key)
	}

	switch key {
	case "current-context":
		return setCurrentContext(cfg, value)
	case "git.server":
		if err := validateGitServer(value); err != nil {
			return err
		}
		ensureGit(cfg).Server = value
	case "git.hostname":
		ensureGit(cfg).Hostname = value
	case "git.owner":
		ensureGit(cfg).Owner = value
	case "git.repository":
		ensureGit(cfg).Repository = value
	case "git.branch":
		ensureGit(cfg).Branch = value
	case "git.clusterPath":
		ensureGit(cfg).ClusterPath = value
	case "git.pat":
		ensureGit(cfg).PAT = value
	case "registry.repository":
		ensureRegistry(cfg).Repository = value
	case "registry.credentials":
		if !strings.Contains(value, ":") {
			return cli.Exitf(cli.ExitUsage, "registry credentials must be in user:pass format")
		}
		ensureRegistry(cfg).Credentials = value
	case "base.url":
		ensureBase(cfg).URL = value
	case "base.branch":
		ensureBase(cfg).Branch = value
	case "base.pat":
		ensureBase(cfg).PAT = value
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
	cl, ok := cfg.Clusters[name]
	if !ok {
		cl = &config.Cluster{ClusterName: name}
		cfg.Clusters[name] = cl
	}
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
	case "minikube.cpus":
		ensureMinikube(cl).CPUs = atoiOr(value, config.DefaultMinikubeCPUs)
	case "minikube.memoryMB":
		ensureMinikube(cl).MemoryMB = atoiOr(value, config.DefaultMinikubeMemory)
	default:
		return cli.Exitf(cli.ExitUsage, "unknown configuration key %q", key)
	}
	return nil
}

func unsetConfigValue(cfg *config.Config, key string) error {
	switch key {
	case "current-context":
		cfg.CurrentContext = ""
	case "git.pat":
		if cfg.Git != nil {
			cfg.Git.PAT = ""
		}
	case "registry.credentials":
		if cfg.Registry != nil {
			cfg.Registry.Credentials = ""
		}
	case "base.pat":
		if cfg.Base != nil {
			cfg.Base.PAT = ""
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

func ensureGit(cfg *config.Config) *config.Git {
	if cfg.Git == nil {
		cfg.Git = &config.Git{}
	}
	return cfg.Git
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
