package config

import (
	"regexp"
	"strings"
)

var (
	clusterNameRe = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]*[a-z0-9])?$`)
	domainRe      = regexp.MustCompile(`^[A-Za-z0-9]([A-Za-z0-9._-]*[A-Za-z0-9])?$`)
)

// Validate checks required keys, enums and value shapes, returning the first
// *ValidationError with an exact key path.
func (c *Config) Validate() error {
	if c.APIVersion != "" && c.APIVersion != APIVersion {
		return &ValidationError{Path: "apiVersion", Msg: "must be " + APIVersion}
	}
	if c.Kind != "" && c.Kind != Kind {
		return &ValidationError{Path: "kind", Msg: "must be " + Kind}
	}
	if c.CurrentContext != "" {
		if _, ok := c.Clusters[c.CurrentContext]; !ok && len(c.Clusters) > 0 {
			return &ValidationError{Path: "current-context", Msg: "refers to an unknown cluster " + c.CurrentContext}
		}
	}
	for name, cl := range c.Clusters {
		if cl == nil {
			return &ValidationError{Path: "clusters." + name, Msg: "must not be empty"}
		}
		if err := validateCluster(name, cl); err != nil {
			return err
		}
	}
	if c.Git != nil && c.Git.Server != "" {
		switch c.Git.Server {
		case ServerGitHub, ServerGitea, ServerGitLab:
		default:
			return &ValidationError{Path: "git.server", Msg: "must be github, gitea or gitlab"}
		}
	}
	return nil
}

func validateCluster(name string, cl *Cluster) error {
	path := "clusters." + name
	switch cl.Provider {
	case ProviderMinikube, ProviderVCluster:
	default:
		return &ValidationError{Path: path + ".provider", Msg: "must be minikube or vcluster"}
	}
	if cl.ClusterName != "" && !clusterNameRe.MatchString(cl.ClusterName) {
		return &ValidationError{Path: path + ".clusterName", Msg: "use lowercase letters, digits and '-' (max 63 chars)"}
	}
	if len(cl.ClusterName) > 63 {
		return &ValidationError{Path: path + ".clusterName", Msg: "must be at most 63 characters"}
	}
	if cl.Domain != "" && !domainRe.MatchString(cl.Domain) {
		return &ValidationError{Path: path + ".domain", Msg: "use letters, digits, '.', '_' and '-'"}
	}
	if cl.Provider == ProviderVCluster {
		if cl.VCluster == nil || strings.TrimSpace(cl.VCluster.HostContext) == "" {
			return &ValidationError{Path: path + ".vcluster.hostContext", Msg: "is required for the vcluster provider"}
		}
		switch cl.VCluster.APIServerServiceType {
		case "", VClusterServiceTypeLoadBalancer, VClusterServiceTypeClusterIP:
		default:
			return &ValidationError{Path: path + ".vcluster.apiServerServiceType", Msg: "must be LoadBalancer or ClusterIP"}
		}
	}
	return nil
}

// ValidateClusterName reports an error when name is not a valid cluster name.
func ValidateClusterName(name string) error {
	if !clusterNameRe.MatchString(name) || len(name) > 63 {
		return &ValidationError{Path: "clusterName", Msg: "invalid cluster name '" + name + "'; use lowercase letters, digits and '-' (max 63 characters)"}
	}
	return nil
}

// ValidateDomain reports an error when domain is not a valid LeptoStack domain.
func ValidateDomain(domain string) error {
	if !domainRe.MatchString(domain) {
		return &ValidationError{Path: "domain", Msg: "invalid LeptoStack domain '" + domain + "'; use letters, digits, '.', '_' and '-'"}
	}
	return nil
}
