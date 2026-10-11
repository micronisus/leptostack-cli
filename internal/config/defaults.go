package config

import (
	"os"
	"path/filepath"
)

// Defaults used when a value is not configured.
const (
	DefaultProvider       = ProviderMinikube
	DefaultClusterName    = "local"
	DefaultLeptostackName = "portal"
	DefaultMinikubeCPUs   = 8
	DefaultMinikubeMemory = 24576
	DefaultGitBranch      = "main"
	DefaultGitServer      = ServerGitHub
	DefaultRegistryRepo   = "ghcr.io"

	DefaultTemplateURL    = "https://github.com/leptostack-fluxcd/cluster-template.git"
	DefaultTemplateBranch = "main"
)

// AutoRepository is the sentinel repository value that means "create the
// repository automatically on start". It is the default in config init.
const AutoRepository = "auto"

// Environment variable names honored as overrides.
const (
	EnvConfigPath          = "LEPTOSTACK_CONFIG"
	EnvGitPAT              = "LEPTOSTACK_GIT_PAT"
	EnvRegistryCredentials = "LEPTOSTACK_REGISTRY_CREDENTIALS"
	EnvTemplatePAT         = "LEPTOSTACK_TEMPLATE_PAT"
	EnvResourcesPAT        = "RESOURCES_GIT_PAT"
	EnvGitServer           = "LEPTOSTACK_GIT_SERVER"
	EnvGitOwner            = "LEPTOSTACK_GIT_OWNER"
	EnvGitRepo             = "LEPTOSTACK_GIT_REPO"
	EnvGitHostname         = "LEPTOSTACK_GIT_HOSTNAME"
	EnvGitBranch           = "LEPTOSTACK_GIT_BRANCH"
	EnvClusterPath         = "LEPTOSTACK_CLUSTER_PATH"
	EnvDomain              = "LEPTOSTACK_DOMAIN"
	EnvClusterName         = "LEPTOSTACK_CLUSTER_NAME"
	EnvTemplateURL         = "LEPTOSTACK_TEMPLATE_GIT_URL"
	EnvTemplateBranch      = "LEPTOSTACK_TEMPLATE_GIT_BRANCH"
)

// HomeDir returns the user's home directory, or "." if undeterminable.
func HomeDir() string {
	if h, err := os.UserHomeDir(); err == nil && h != "" {
		return h
	}
	if h := os.Getenv("HOME"); h != "" {
		return h
	}
	return "."
}

// Dir returns the configuration directory (~/.config/leptostack).
func Dir() string {
	return filepath.Join(HomeDir(), ".config", "leptostack")
}

// DefaultPath returns the YAML config path, honoring LEPTOSTACK_CONFIG.
func DefaultPath() string {
	if p := os.Getenv(EnvConfigPath); p != "" {
		return p
	}
	return filepath.Join(Dir(), "config.yaml")
}

// LegacyPath returns the path of the legacy bash KV config file.
func LegacyPath() string {
	return filepath.Join(Dir(), "config")
}

// DefaultDomain returns the default LeptoStack domain (<user>-leptostack.test).
func DefaultDomain() string {
	user := os.Getenv("USER")
	if user == "" {
		user = "user"
	}
	return user + "-leptostack.test"
}

// DefaultHostNamespace returns the default vcluster host namespace (the user).
func DefaultHostNamespace() string {
	if ns := os.Getenv("USER"); ns != "" {
		return ns
	}
	return "default"
}
