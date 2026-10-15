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

	DefaultBaseURL    = "https://github.com/leptostack-fluxcd/leptostack-base.git"
	DefaultBaseBranch = "main"
)

// AutoRepository is the sentinel repository value that means "create the
// repository automatically on start". It is the default in config init.
const AutoRepository = "auto"

// Environment variable names honored as overrides.
const (
	EnvConfigPath          = "LEPTOSTACK_CONFIG"
	EnvConfigDir           = "LEPTOSTACK_CONFIG_DIR"
	EnvGitPAT              = "LEPTOSTACK_GIT_PAT"
	EnvRegistryCredentials = "LEPTOSTACK_REGISTRY_CREDENTIALS"
	EnvBasePAT             = "LEPTOSTACK_BASE_PAT"
	EnvResourcesPAT        = "RESOURCES_GIT_PAT"
	EnvGitServer           = "LEPTOSTACK_GIT_SERVER"
	EnvGitOwner            = "LEPTOSTACK_GIT_OWNER"
	EnvGitRepo             = "LEPTOSTACK_GIT_REPO"
	EnvGitHostname         = "LEPTOSTACK_GIT_HOSTNAME"
	EnvGitBranch           = "LEPTOSTACK_GIT_BRANCH"
	EnvClusterPath         = "LEPTOSTACK_CLUSTER_PATH"
	EnvDomain              = "LEPTOSTACK_DOMAIN"
	EnvClusterName         = "LEPTOSTACK_CLUSTER_NAME"
	EnvBaseURL             = "LEPTOSTACK_BASE_URL"
	EnvBaseBranch          = "LEPTOSTACK_BASE_BRANCH"
	EnvGitCAFile           = "LEPTOSTACK_GIT_CA_FILE"
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

// username returns the current user's name. Unix shells export USER while
// Windows sets USERNAME, so both are honored (USER first to keep Unix behavior
// byte-identical).
func username() string {
	if u := os.Getenv("USER"); u != "" {
		return u
	}
	return os.Getenv("USERNAME")
}

// DefaultDomain returns the default LeptoStack domain (<user>-leptostack.test).
func DefaultDomain() string {
	user := username()
	if user == "" {
		user = "user"
	}
	return user + "-leptostack.test"
}

// DefaultHostNamespace returns the default vcluster host namespace (the user).
func DefaultHostNamespace() string {
	if ns := username(); ns != "" {
		return ns
	}
	return "default"
}
