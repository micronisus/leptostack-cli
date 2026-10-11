// Package secrets generates Kubernetes Secret manifests for the create secret
// workflow with the canonical key names per kind, and provides value redaction.
package secrets

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/micronisus/leptostack-cli/internal/yamlquote"
)

// Kind identifiers.
const (
	KindGit            = "git"
	KindDockerRegistry = "docker-registry"
	KindTLS            = "tls"
	KindOpaque         = "opaque"
)

// SupportedKinds lists the secret kinds the CLI can create.
func SupportedKinds() []string {
	return []string{KindGit, KindDockerRegistry, KindTLS, KindOpaque}
}

// Request describes a secret to create.
type Request struct {
	Kind      string
	Name      string
	Namespace string
	Username  string
	Password  string
	Token     string
	Registry  string
	CertFile  string
	KeyFile   string
	Literals  map[string]string
	Files     map[string]string
}

// Validate checks the request against the kind's requirements.
func (r Request) Validate() error {
	if strings.TrimSpace(r.Name) == "" {
		return fmt.Errorf("secret name is required")
	}
	switch r.Kind {
	case KindGit:
		if r.Password == "" && r.Token == "" {
			return fmt.Errorf("git secrets require --password, --token or --password-stdin")
		}
	case KindDockerRegistry:
		if r.Registry == "" {
			return fmt.Errorf("--registry is required for docker-registry secrets")
		}
		if r.Username == "" || r.Password == "" {
			return fmt.Errorf("docker-registry secrets require --username and --password")
		}
	case KindTLS:
		if r.CertFile == "" || r.KeyFile == "" {
			return fmt.Errorf("tls secrets require --cert and --key")
		}
	case KindOpaque:
		if len(r.Literals) == 0 && len(r.Files) == 0 {
			return fmt.Errorf("opaque secrets require at least one --from-literal or --from-file")
		}
	default:
		return fmt.Errorf("unknown secret kind %q (expected one of %v)", r.Kind, SupportedKinds())
	}
	return nil
}

// Keys returns the canonical data keys this secret will carry.
func (r Request) Keys() ([]string, error) {
	switch r.Kind {
	case KindGit:
		return []string{"username", "password"}, nil
	case KindDockerRegistry:
		return []string{".dockerconfigjson"}, nil
	case KindTLS:
		return []string{"tls.crt", "tls.key"}, nil
	case KindOpaque:
		keys := []string{}
		for k := range r.Literals {
			keys = append(keys, k)
		}
		for k := range r.Files {
			keys = append(keys, k)
		}
		return keys, nil
	}
	return nil, fmt.Errorf("unknown secret kind %q", r.Kind)
}

// ManifestStringData builds an arbitrary stringData secret manifest.
func ManifestStringData(name, namespace, secretType string, data map[string]string) string {
	if secretType == "" {
		secretType = "Opaque"
	}
	var b strings.Builder
	b.WriteString("apiVersion: v1\nkind: Secret\nmetadata:\n")
	fmt.Fprintf(&b, "  name: %s\n  namespace: %s\n", name, namespace)
	fmt.Fprintf(&b, "type: %s\n", secretType)
	b.WriteString("stringData:\n")
	for _, k := range sortedKeys(data) {
		fmt.Fprintf(&b, "  %s: %s\n", k, quote(data[k]))
	}
	return b.String()
}

// Manifest renders the secret manifest as YAML (values under stringData).
func (r Request) Manifest() (string, error) {
	if err := r.Validate(); err != nil {
		return "", err
	}
	ns := r.Namespace
	if ns == "" {
		ns = "flux-system"
	}

	data, secretType, err := r.buildData()
	if err != nil {
		return "", err
	}

	var b strings.Builder
	b.WriteString("apiVersion: v1\nkind: Secret\nmetadata:\n")
	fmt.Fprintf(&b, "  name: %s\n  namespace: %s\n", r.Name, ns)
	if secretType != "Opaque" {
		fmt.Fprintf(&b, "type: %s\n", secretType)
	} else {
		b.WriteString("type: Opaque\n")
	}
	b.WriteString("stringData:\n")
	// Deterministic order.
	for _, k := range sortedKeys(data) {
		fmt.Fprintf(&b, "  %s: %s\n", k, quote(data[k]))
	}
	return b.String(), nil
}

func (r Request) buildData() (map[string]string, string, error) {
	switch r.Kind {
	case KindGit:
		username := r.Username
		password := r.Password
		if r.Token != "" {
			password = r.Token
			if username == "" {
				username = "git"
			}
		}
		if username == "" {
			username = "git"
		}
		return map[string]string{"username": username, "password": password}, "Opaque", nil
	case KindDockerRegistry:
		auth := base64.StdEncoding.EncodeToString([]byte(r.Username + ":" + r.Password))
		cfg := map[string]any{
			"auths": map[string]any{
				r.Registry: map[string]any{
					"auth":     auth,
					"username": r.Username,
					"password": r.Password,
				},
			},
		}
		payload, err := json.Marshal(cfg)
		if err != nil {
			return nil, "", err
		}
		return map[string]string{".dockerconfigjson": string(payload)}, "kubernetes.io/dockerconfigjson", nil
	case KindTLS:
		crt, err := os.ReadFile(r.CertFile)
		if err != nil {
			return nil, "", fmt.Errorf("reading --cert: %w", err)
		}
		key, err := os.ReadFile(r.KeyFile)
		if err != nil {
			return nil, "", fmt.Errorf("reading --key: %w", err)
		}
		return map[string]string{"tls.crt": string(crt), "tls.key": string(key)}, "kubernetes.io/tls", nil
	case KindOpaque:
		data := map[string]string{}
		for k, v := range r.Literals {
			data[k] = v
		}
		for k, path := range r.Files {
			b, err := os.ReadFile(path)
			if err != nil {
				return nil, "", fmt.Errorf("reading --from-file %s: %w", k, err)
			}
			data[k] = string(b)
		}
		return data, "Opaque", nil
	}
	return nil, "", fmt.Errorf("unknown secret kind %q", r.Kind)
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	for i := 1; i < len(keys); i++ {
		for j := i; j > 0 && keys[j-1] > keys[j]; j-- {
			keys[j-1], keys[j] = keys[j], keys[j-1]
		}
	}
	return keys
}

func quote(s string) string {
	return yamlquote.Quote(s)
}
