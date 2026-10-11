// Package sources generates and validates Flux source manifests
// (source.toolkit.fluxcd.io) for the create/delete source workflows.
package sources

import (
	"fmt"
	"strings"

	"github.com/micronisus/leptostack-cli/internal/yamlquote"
)

// APIGroup is the Flux source API group.
const APIGroup = "source.toolkit.fluxcd.io"

// Type identifiers.
const (
	TypeGit    = "git"
	TypeHelm   = "helm"
	TypeBucket = "bucket"
	TypeOCI    = "oci"
)

// Kind maps a source type to its Kubernetes kind.
func Kind(t string) (string, error) {
	switch t {
	case TypeGit:
		return "GitRepository", nil
	case TypeHelm:
		return "HelmRepository", nil
	case TypeBucket:
		return "Bucket", nil
	case TypeOCI:
		return "OCIRepository", nil
	}
	return "", fmt.Errorf("unknown source type %q (expected git, helm, bucket or oci)", t)
}

// RequiredSecretKeys returns the secret key names acceptable for a source type.
// A referenced secret must contain at least one of the listed compatible sets.
func RequiredSecretKeys(t string) [][]string {
	switch t {
	case TypeGit:
		return [][]string{{"username", "password"}, {"identity", "identity.pub"}, {"bearerToken"}}
	case TypeHelm:
		return [][]string{{"username", "password"}, {"certFile", "keyFile"}, {"caFile"}}
	case TypeOCI:
		return [][]string{{"username", "password"}}
	case TypeBucket:
		return [][]string{{"accesskey", "secretkey"}, {"secretKey"}, {"accessKey"}}
	}
	return nil
}

// Request describes a source to create.
type Request struct {
	Type            string
	Name            string
	Namespace       string
	URL             string
	Branch          string
	Tag             string
	Interval        string
	SecretRef       string
	BucketName      string
	Region          string
	Provider        string
	PassCredentials bool
	Insecure        bool
}

// Validate checks the request fields for the source type.
func (r Request) Validate() error {
	if _, err := Kind(r.Type); err != nil {
		return err
	}
	if strings.TrimSpace(r.Name) == "" {
		return fmt.Errorf("source name is required")
	}
	if strings.TrimSpace(r.URL) == "" {
		return fmt.Errorf("--url is required")
	}
	if r.Branch != "" && r.Tag != "" {
		return fmt.Errorf("--branch and --tag are mutually exclusive")
	}
	if r.Type == TypeBucket && strings.TrimSpace(r.BucketName) == "" {
		return fmt.Errorf("--bucket-name is required for bucket sources")
	}
	if r.Interval == "" {
		r.Interval = "5m"
	}
	return nil
}

// Manifest renders the source manifest as YAML.
func (r Request) Manifest() (string, error) {
	kind, err := Kind(r.Type)
	if err != nil {
		return "", err
	}
	ns := r.Namespace
	if ns == "" {
		ns = "flux-system"
	}
	interval := r.Interval
	if interval == "" {
		interval = "5m"
	}

	var b strings.Builder
	fmt.Fprintf(&b, "apiVersion: %s/v1\nkind: %s\n", APIGroup, kind)
	b.WriteString("metadata:\n")
	fmt.Fprintf(&b, "  name: %s\n  namespace: %s\n", r.Name, ns)
	b.WriteString("spec:\n")
	fmt.Fprintf(&b, "  interval: %s\n", interval)
	// Bucket uses `endpoint`; the repository kinds use `url`.
	if r.Type == TypeBucket {
		fmt.Fprintf(&b, "  endpoint: %s\n", quote(r.URL))
	} else {
		fmt.Fprintf(&b, "  url: %s\n", quote(r.URL))
	}

	switch r.Type {
	case TypeGit:
		if r.Branch != "" || r.Tag != "" {
			b.WriteString("  ref:\n")
			if r.Branch != "" {
				fmt.Fprintf(&b, "    branch: %s\n", quote(r.Branch))
			}
			if r.Tag != "" {
				fmt.Fprintf(&b, "    tag: %s\n", quote(r.Tag))
			}
		}
	case TypeBucket:
		fmt.Fprintf(&b, "  bucketName: %s\n", quote(r.BucketName))
		if r.Region != "" {
			fmt.Fprintf(&b, "  region: %s\n", quote(r.Region))
		}
		if r.Provider != "" {
			fmt.Fprintf(&b, "  provider: %s\n", quote(r.Provider))
		}
	case TypeHelm:
		if r.PassCredentials {
			b.WriteString("  passCredentials: true\n")
		}
	case TypeOCI:
		if r.Insecure {
			b.WriteString("  insecure: true\n")
		}
	}

	if r.SecretRef != "" {
		b.WriteString("  secretRef:\n")
		fmt.Fprintf(&b, "    name: %s\n", r.SecretRef)
	}

	return b.String(), nil
}

// ValidateSecretRef checks that a referenced secret exists and carries keys
// compatible with the source type.
func (r Request) ValidateSecretRef(secretKeys []string, found bool) error {
	if r.SecretRef == "" {
		return nil
	}
	if !found {
		return fmt.Errorf("secretRef %q does not exist in namespace %q; create it first with 'leptostack create secret'", r.SecretRef, namespaceOr(r.Namespace))
	}
	sets := RequiredSecretKeys(r.Type)
	have := map[string]bool{}
	for _, k := range secretKeys {
		have[k] = true
	}
	for _, set := range sets {
		ok := true
		for _, k := range set {
			if !have[k] {
				ok = false
				break
			}
		}
		if ok {
			return nil
		}
	}
	return fmt.Errorf("secretRef %q exists but is missing keys required by a %s source (expected one of %v; found %v)", r.SecretRef, r.Type, sets, secretKeys)
}

func namespaceOr(ns string) string {
	if ns == "" {
		return "flux-system"
	}
	return ns
}

func quote(s string) string { return yamlquote.Quote(s) }
