// Package config implements the typed, validatable, multi-cluster YAML
// configuration file, its migration from the legacy bash KV file, secret
// redaction and environment-variable overrides.
package config

// Kind and APIVersion identify the config document.
const (
	APIVersion = "v1"
	Kind       = "LeptostackConfig"
)

// Provider identifiers.
const (
	ProviderMinikube = "minikube"
	ProviderVCluster = "vcluster"
)

// Git server identifiers.
const (
	ServerGitHub = "github"
	ServerGitea  = "gitea"
	ServerGitLab = "gitlab"
)

// VCluster API server Service types.
const (
	VClusterServiceTypeLoadBalancer = "LoadBalancer"
	VClusterServiceTypeClusterIP    = "ClusterIP"
)

// Config is the root of the configuration document.
type Config struct {
	APIVersion     string              `json:"apiVersion"`
	Kind           string              `json:"kind"`
	CurrentContext string              `json:"current-context,omitempty"`
	Clusters       map[string]*Cluster `json:"clusters,omitempty"`
	Git            *Git                `json:"git,omitempty"`
	Registry       *Registry           `json:"registry,omitempty"`
	Base           *Base               `json:"base,omitempty"`
	Preferences    *Preferences        `json:"preferences,omitempty"`
}

// Cluster is one named cluster entry.
type Cluster struct {
	Provider       string    `json:"provider"`
	ClusterName    string    `json:"clusterName,omitempty"`
	LeptostackName string    `json:"leptostackName,omitempty"`
	Domain         string    `json:"domain,omitempty"`
	Minikube       *Minikube `json:"minikube,omitempty"`
	VCluster       *VCluster `json:"vcluster,omitempty"`
	Git            *Git      `json:"git,omitempty"`
	Registry       *Registry `json:"registry,omitempty"`
	Base           *Base     `json:"base,omitempty"`
}

// Minikube holds minikube-specific settings.
type Minikube struct {
	CPUs     int `json:"cpus,omitempty"`
	MemoryMB int `json:"memoryMB,omitempty"`
}

// VCluster holds vcluster-specific settings. The generated kubectl context is
// runtime state cached per cluster under the runtime dir, not configuration.
type VCluster struct {
	HostContext   string `json:"hostContext,omitempty"`
	HostNamespace string `json:"hostNamespace,omitempty"`
	// Metrics toggles the vcluster metrics-server integration. Unset means
	// enabled (the default).
	Metrics *bool `json:"metricsServer,omitempty"`
	// OIDC toggles vcluster API server OIDC authentication. Unset means enabled
	// (the default).
	OIDC *bool `json:"oidc,omitempty"`
	// APIServerServiceType is the Kubernetes Service type of the vcluster API
	// server: LoadBalancer or ClusterIP. Unset means LoadBalancer (the default).
	APIServerServiceType string `json:"apiServerServiceType,omitempty"`
}

// MetricsEnabled reports whether the vcluster metrics-server integration is on.
// An unset value defaults to enabled.
func (v VCluster) MetricsEnabled() bool { return v.Metrics == nil || *v.Metrics }

// OIDCEnabled reports whether vcluster API server OIDC authentication is on. An
// unset value defaults to enabled.
func (v VCluster) OIDCEnabled() bool { return v.OIDC == nil || *v.OIDC }

// APIServerServiceTypeOrDefault returns the API server Service type. An unset
// value defaults to LoadBalancer.
func (v VCluster) APIServerServiceTypeOrDefault() string {
	if v.APIServerServiceType == "" {
		return VClusterServiceTypeLoadBalancer
	}
	return v.APIServerServiceType
}

// Git holds the GitOps repository configuration.
type Git struct {
	Server      string `json:"server,omitempty"`
	Hostname    string `json:"hostname,omitempty"`
	Owner       string `json:"owner,omitempty"`
	Repository  string `json:"repository,omitempty"`
	Branch      string `json:"branch,omitempty"`
	ClusterPath string `json:"clusterPath,omitempty"`
	PAT         string `json:"pat,omitempty"`
}

// Registry holds the module image registry configuration.
type Registry struct {
	Repository  string `json:"repository,omitempty"`
	Credentials string `json:"credentials,omitempty"`
}

// Base identifies the private leptostack-base repository: the GitRepository
// source the embedded cluster template reconciles its infrastructure from.
type Base struct {
	URL    string `json:"url,omitempty"`
	Branch string `json:"branch,omitempty"`
	PAT    string `json:"pat,omitempty"`
}

// Preferences holds user preferences.
type Preferences struct {
	AutoUpgradeCheck *bool `json:"autoUpgradeCheck,omitempty"`
}

// Resolved is the merged, per-cluster view used by commands. Per-cluster values
// win over global values, which win over defaults; environment variables
// override secrets.
type Resolved struct {
	Name            string
	Provider        string
	ClusterName     string
	LeptostackName  string
	Domain          string
	Minikube        Minikube
	VCluster        VCluster
	Git             Git
	Registry        Registry
	Base            Base
	MinikubeProfile string
	KubeContext     string
	HostNamespace   string
	// AutoRepo is true when the GitOps repository is the "auto" sentinel and
	// has not yet been materialized by the forge on start.
	AutoRepo bool
}

// Clone returns a deep copy of the config.
func (c *Config) Clone() *Config {
	if c == nil {
		return nil
	}
	out := *c
	if c.Clusters != nil {
		out.Clusters = make(map[string]*Cluster, len(c.Clusters))
		for k, v := range c.Clusters {
			out.Clusters[k] = v.clone()
		}
	}
	if c.Git != nil {
		g := *c.Git
		out.Git = &g
	}
	if c.Registry != nil {
		r := *c.Registry
		out.Registry = &r
	}
	if c.Base != nil {
		b := *c.Base
		out.Base = &b
	}
	if c.Preferences != nil {
		p := *c.Preferences
		out.Preferences = &p
	}
	return &out
}

// Clone returns a deep copy of the cluster entry.
func (cl *Cluster) Clone() *Cluster { return cl.clone() }

func (cl *Cluster) clone() *Cluster {
	if cl == nil {
		return nil
	}
	out := *cl
	if cl.Minikube != nil {
		m := *cl.Minikube
		out.Minikube = &m
	}
	if cl.VCluster != nil {
		v := *cl.VCluster
		out.VCluster = &v
	}
	if cl.Git != nil {
		g := *cl.Git
		out.Git = &g
	}
	if cl.Registry != nil {
		r := *cl.Registry
		out.Registry = &r
	}
	if cl.Base != nil {
		b := *cl.Base
		out.Base = &b
	}
	return &out
}
