package config

import (
	"bufio"
	"os"
	"strconv"
	"strings"

	"github.com/micronisus/leptostack-cli/internal/runtime"
)

// ImportLegacy reads the bash KV config and maps it one-to-one into a Config
// with a single named cluster. The original file is left untouched.
func ImportLegacy(path string) (*Config, error) {
	kv, err := parseKVFile(path)
	if err != nil {
		return nil, err
	}

	clusterName := kv["CLUSTER_NAME"]
	if clusterName == "" {
		clusterName = DefaultClusterName
	}
	provider := kv["CLUSTER_PROVIDER"]
	if provider == "" {
		provider = DefaultProvider
	}
	domain := kv["LEPTOSTACK_DOMAIN"]
	if domain == "" {
		domain = DefaultDomain()
	}
	leptostackName := kv["LEPTOSTACK_NAME"]
	if leptostackName == "" {
		leptostackName = DefaultLeptostackName
	}

	cl := &Cluster{
		Provider:       provider,
		ClusterName:    clusterName,
		LeptostackName: leptostackName,
		Domain:         domain,
	}
	if provider == ProviderMinikube {
		cl.Minikube = &Minikube{
			CPUs:     atoiDefault(kv["MINIKUBE_CPUS"], DefaultMinikubeCPUs),
			MemoryMB: atoiDefault(kv["MINIKUBE_MEMORY"], DefaultMinikubeMemory),
		}
	}
	if provider == ProviderVCluster {
		cl.VCluster = &VCluster{
			HostContext:   kv["HOST_CONTEXT"],
			HostNamespace: kv["HOST_NAMESPACE"],
		}
		if cl.VCluster.HostNamespace == "" {
			cl.VCluster.HostNamespace = DefaultHostNamespace()
		}
		// The generated vcluster context is runtime state, cached per cluster.
		if ctx := kv["VCLUSTER_CONTEXT"]; ctx != "" {
			_ = runtime.WriteVClusterContext(clusterName, ctx)
		}
	}

	branch := kv["GIT_BRANCH"]
	if branch == "" {
		branch = DefaultGitBranch
	}
	clusterPath := kv["CLUSTER_PATH"]
	if clusterPath == "" {
		clusterPath = "clusters/" + clusterName
	}

	cfg := &Config{
		APIVersion:     APIVersion,
		Kind:           Kind,
		CurrentContext: clusterName,
		Clusters:       map[string]*Cluster{clusterName: cl},
		Git: &Git{
			Server:      kv["GIT_SERVER"],
			Hostname:    kv["GIT_HOSTNAME"],
			Owner:       kv["GIT_OWNER"],
			Repository:  kv["GIT_REPO"],
			Branch:      branch,
			ClusterPath: clusterPath,
			PAT:         kv["GIT_PAT"],
		},
		Registry: &Registry{
			Repository:  kv["MODULE_REGISTRY_REPOSITORY"],
			Credentials: kv["MODULE_REGISTRY_CREDENTIALS"],
		},
		Template: &Template{
			URL:    kv["TEMPLATE_GIT_URL"],
			Branch: kv["TEMPLATE_GIT_BRANCH"],
			PAT:    firstNonEmpty(kv["RESOURCES_GIT_PAT"]),
		},
	}
	cfg.ApplyDefaults()
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return cfg, nil
}

func firstNonEmpty(v ...string) string {
	for _, s := range v {
		if s != "" {
			return s
		}
	}
	return ""
}

// parseKVFile parses KEY="value" / KEY=value lines, ignoring comments and
// blank lines. Surrounding quotes are removed and common escapes undone.
func parseKVFile(path string) (map[string]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	kv := map[string]string{}
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		idx := strings.IndexByte(line, '=')
		if idx < 0 {
			continue
		}
		key := strings.TrimSpace(line[:idx])
		val := strings.TrimSpace(line[idx+1:])
		val = unquoteKV(val)
		kv[key] = val
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	return kv, nil
}

func unquoteKV(v string) string {
	if len(v) >= 2 && v[0] == '"' && v[len(v)-1] == '"' {
		inner := v[1 : len(v)-1]
		inner = strings.ReplaceAll(inner, `\"`, `"`)
		inner = strings.ReplaceAll(inner, `\\`, `\`)
		return inner
	}
	if len(v) >= 2 && v[0] == '\'' && v[len(v)-1] == '\'' {
		return v[1 : len(v)-1]
	}
	return v
}

func atoiDefault(s string, def int) int {
	if s == "" {
		return def
	}
	n, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil || n <= 0 {
		return def
	}
	return n
}
