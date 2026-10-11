package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"sigs.k8s.io/yaml"
)

// ErrNotConfigured is returned when no configuration exists yet.
var ErrNotConfigured = errors.New("configuration not found")

// ValidationError describes an invalid field inside the config document.
type ValidationError struct {
	Path string
	Msg  string
}

func (e *ValidationError) Error() string {
	if e.Path == "" {
		return "invalid configuration: " + e.Msg
	}
	return fmt.Sprintf("invalid configuration at %s: %s", e.Path, e.Msg)
}

// Load reads the config from the default path, importing the legacy KV file on
// first run if needed.
func Load() (*Config, error) {
	c, _, err := LoadWithMigration()
	return c, err
}

// LoadWithMigration reads the config and reports whether a legacy import ran.
func LoadWithMigration() (*Config, bool, error) {
	path := DefaultPath()
	migrated := false

	if _, err := os.Stat(path); err != nil {
		if !os.IsNotExist(err) {
			return nil, false, err
		}
		legacy := LegacyPath()
		if _, lerr := os.Stat(legacy); lerr == nil {
			cfg, ierr := ImportLegacy(legacy)
			if ierr != nil {
				return nil, false, ierr
			}
			if serr := cfg.SaveTo(path); serr != nil {
				return nil, false, serr
			}
			migrated = true
		} else {
			return nil, false, fmt.Errorf("%w: run 'leptostack config init' first", ErrNotConfigured)
		}
	}

	cfg, err := LoadFrom(path)
	if err != nil {
		return nil, migrated, err
	}
	return cfg, migrated, nil
}

// LoadFrom reads and validates a config file from an explicit path.
func LoadFrom(path string) (*Config, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("%w: %s", ErrNotConfigured, path)
		}
		return nil, err
	}
	var cfg Config
	b, err = stripDeprecatedFields(b)
	if err != nil {
		return nil, fmt.Errorf("parsing %s: %w", path, err)
	}
	if err := yaml.UnmarshalStrict(b, &cfg); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", path, err)
	}
	cfg.ApplyDefaults()
	if cfg.Clusters == nil {
		cfg.Clusters = map[string]*Cluster{}
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return &cfg, nil
}

// stripDeprecatedFields removes keys that were removed from the schema so older
// configuration files keep loading under strict unmarshalling. Currently it
// drops clusters.*.vcluster.dnsTargetIP, which the CLI no longer uses.
func stripDeprecatedFields(b []byte) ([]byte, error) {
	var raw map[string]any
	if err := yaml.Unmarshal(b, &raw); err != nil {
		// Return the original bytes so the strict unmarshal reports the error.
		return b, nil
	}
	changed := false
	if clusters, ok := raw["clusters"].(map[string]any); ok {
		for _, entry := range clusters {
			cluster, ok := entry.(map[string]any)
			if !ok {
				continue
			}
			if vc, ok := cluster["vcluster"].(map[string]any); ok {
				if _, present := vc["dnsTargetIP"]; present {
					delete(vc, "dnsTargetIP")
					changed = true
				}
			}
		}
	}
	if !changed {
		return b, nil
	}
	return yaml.Marshal(raw)
}

// Save writes the config to the default path.
func (c *Config) Save() error { return c.SaveTo(DefaultPath()) }

// SaveTo writes the config to path with mode 0600.
func (c *Config) SaveTo(path string) error {
	c.ApplyDefaults()
	if err := c.Validate(); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	b, err := yaml.Marshal(c)
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o600)
}

// ApplyDefaults fills unset values with their defaults so partially-written
// configuration files keep working.
func (c *Config) ApplyDefaults() {
	if c.APIVersion == "" {
		c.APIVersion = APIVersion
	}
	if c.Kind == "" {
		c.Kind = Kind
	}
	if c.Clusters == nil {
		c.Clusters = map[string]*Cluster{}
	}
	for name, cl := range c.Clusters {
		if cl == nil {
			cl = &Cluster{}
			c.Clusters[name] = cl
		}
		if cl.ClusterName == "" {
			cl.ClusterName = name
		}
		if cl.Provider == "" {
			cl.Provider = DefaultProvider
		}
		if cl.LeptostackName == "" {
			cl.LeptostackName = DefaultLeptostackName
		}
		if cl.Domain == "" {
			cl.Domain = DefaultDomain()
		}
		if cl.Provider == ProviderVCluster {
			if cl.VCluster == nil {
				cl.VCluster = &VCluster{}
			}
			if cl.VCluster.HostNamespace == "" {
				cl.VCluster.HostNamespace = DefaultHostNamespace()
			}
		}
		if cl.Provider == ProviderMinikube {
			if cl.Minikube == nil {
				cl.Minikube = &Minikube{}
			}
			if cl.Minikube.CPUs == 0 {
				cl.Minikube.CPUs = DefaultMinikubeCPUs
			}
			if cl.Minikube.MemoryMB == 0 {
				cl.Minikube.MemoryMB = DefaultMinikubeMemory
			}
		}
	}
	if c.Git == nil {
		c.Git = &Git{}
	}
	if c.Git.Branch == "" {
		c.Git.Branch = DefaultGitBranch
	}
	if c.Git.Server == "" {
		c.Git.Server = DefaultGitServer
	}
	if c.Registry == nil {
		c.Registry = &Registry{}
	}
	if c.Registry.Repository == "" {
		c.Registry.Repository = DefaultRegistryRepo
	}
	if c.Template == nil {
		c.Template = &Template{}
	}
	if c.Template.URL == "" {
		c.Template.URL = DefaultTemplateURL
	}
	if c.Template.Branch == "" {
		c.Template.Branch = DefaultTemplateBranch
	}
}

// CurrentClusterName returns the selected cluster name: current-context, or the
// single configured cluster, or the default name.
func (c *Config) CurrentClusterName() string {
	if c.CurrentContext != "" {
		return c.CurrentContext
	}
	if len(c.Clusters) == 1 {
		for name := range c.Clusters {
			return name
		}
	}
	return DefaultClusterName
}
