package config

import (
	"fmt"
	"os"
	"strings"

	"github.com/micronisus/leptostack-cli/internal/runtime"
)

// Resolve merges the selected cluster with global settings, defaults and
// environment overrides, producing the effective configuration commands use.
func (c *Config) Resolve(name string) (*Resolved, error) {
	if name == "" {
		name = c.CurrentClusterName()
	}
	if len(c.Clusters) == 0 {
		return nil, fmt.Errorf("%w: no clusters configured; run 'leptostack config init'", ErrNotConfigured)
	}
	cl, ok := c.Clusters[name]
	if !ok {
		return nil, &ValidationError{Path: "clusters", Msg: fmt.Sprintf("unknown cluster %q", name)}
	}

	r := &Resolved{Name: name}
	r.Provider = orDefault(cl.Provider, DefaultProvider)
	r.ClusterName = orDefault(cl.ClusterName, name)
	r.LeptostackName = orDefault(cl.LeptostackName, DefaultLeptostackName)
	r.Domain = orDefault(cl.Domain, DefaultDomain())
	if v := os.Getenv(EnvDomain); v != "" {
		r.Domain = v
	}

	// Minikube.
	if cl.Minikube != nil {
		r.Minikube = *cl.Minikube
	}
	if r.Minikube.CPUs == 0 {
		r.Minikube.CPUs = DefaultMinikubeCPUs
	}
	if r.Minikube.MemoryMB == 0 {
		r.Minikube.MemoryMB = DefaultMinikubeMemory
	}

	// vcluster.
	if cl.VCluster != nil {
		r.VCluster = *cl.VCluster
	}
	if r.VCluster.HostNamespace == "" {
		r.VCluster.HostNamespace = DefaultHostNamespace()
	}

	// Git: per-cluster, then env.
	if cl.Git != nil {
		r.Git = *cl.Git
	}
	applyGitEnv(&r.Git)
	if r.Git.Server == "" {
		r.Git.Server = DefaultGitServer
	}
	if r.Git.Branch == "" {
		r.Git.Branch = DefaultGitBranch
	}
	if r.Git.ClusterPath == "" {
		r.Git.ClusterPath = "clusters/" + r.ClusterName
	}

	// Registry.
	if cl.Registry != nil {
		r.Registry = *cl.Registry
	}
	if r.Registry.Repository == "" {
		r.Registry.Repository = DefaultRegistryRepo
	}
	if v := os.Getenv(EnvRegistryCredentials); v != "" {
		r.Registry.Credentials = v
	}

	// Base.
	if cl.Base != nil {
		r.Base = *cl.Base
	}
	if v := os.Getenv(EnvBaseURL); v != "" {
		r.Base.URL = v
	}
	if v := os.Getenv(EnvBaseBranch); v != "" {
		r.Base.Branch = v
	}
	if r.Base.PAT == "" {
		r.Base.PAT = firstNonEmpty(os.Getenv(EnvBasePAT), os.Getenv(EnvResourcesPAT))
	}
	if r.Base.URL == "" {
		r.Base.URL = DefaultBaseURL
	}
	if r.Base.Branch == "" {
		r.Base.Branch = DefaultBaseBranch
	}

	// Derived names.
	r.MinikubeProfile = "leptostack-" + r.ClusterName
	r.HostNamespace = r.VCluster.HostNamespace
	r.KubeContext = deriveContext(r, runtime.ReadVClusterContext(name))
	r.AutoRepo = strings.EqualFold(strings.TrimSpace(r.Git.Repository), AutoRepository)

	return r, nil
}

// deriveContext mirrors get_context_name() from the bash implementation.
func deriveContext(r *Resolved, cachedVClusterContext string) string {
	if r.Provider == ProviderVCluster {
		if cachedVClusterContext != "" {
			return cachedVClusterContext
		}
		return fmt.Sprintf("vcluster_%s_%s_%s", r.ClusterName, r.VCluster.HostNamespace, r.VCluster.HostContext)
	}
	return "leptostack-" + r.ClusterName
}

func applyGitEnv(g *Git) {
	if v := os.Getenv(EnvGitServer); v != "" {
		g.Server = v
	}
	if v := os.Getenv(EnvGitHostname); v != "" {
		g.Hostname = v
	}
	if v := os.Getenv(EnvGitOwner); v != "" {
		g.Owner = v
	}
	if v := os.Getenv(EnvGitRepo); v != "" {
		g.Repository = v
	}
	if v := os.Getenv(EnvGitBranch); v != "" {
		g.Branch = v
	}
	if v := os.Getenv(EnvClusterPath); v != "" {
		g.ClusterPath = v
	}
	if v := os.Getenv(EnvGitPAT); v != "" {
		g.PAT = v
	}
}

// ClonePath expands the {cluster} placeholder in clusterPath.
func (r *Resolved) ClonePath() string {
	return strings.ReplaceAll(r.Git.ClusterPath, "{cluster}", r.ClusterName)
}

func orDefault(v, def string) string {
	if strings.TrimSpace(v) == "" {
		return def
	}
	return v
}
