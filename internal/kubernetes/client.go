// Package kubernetes wraps the kubectl CLI: context resolution/derivation,
// kubeconfig operations and structured reads. The CLI deliberately delegates
// to kubectl rather than importing client-go, keeping behavior identical to
// the bash implementation (see RFC "External tool orchestration").
package kubernetes

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/micronisus/leptostack-cli/internal/execx"
)

// Client runs kubectl with a fixed context and kubeconfig.
type Client struct {
	R           *execx.Runner
	KubeContext string
	Kubeconfig  string
	Namespace   string
}

// New builds a Client.
func New(r *execx.Runner, kubeContext, kubeconfig, namespace string) *Client {
	return &Client{R: r, KubeContext: kubeContext, Kubeconfig: kubeconfig, Namespace: namespace}
}

// Cmd builds an execx.Cmd for kubectl with global context/kubeconfig prepended.
func (c *Client) Cmd(args ...string) execx.Cmd {
	full := []string{}
	if c.Kubeconfig != "" {
		full = append(full, "--kubeconfig", c.Kubeconfig)
	}
	if c.KubeContext != "" {
		full = append(full, "--context", c.KubeContext)
	}
	if c.Namespace != "" {
		full = append(full, "-n", c.Namespace)
	}
	full = append(full, args...)
	return execx.Cmd{Name: "kubectl", Args: full}
}

// Run streams a kubectl command.
func (c *Client) Run(ctx context.Context, args ...string) error {
	return c.R.Run(ctx, c.Cmd(args...))
}

// RunDiscard runs a kubectl command discarding output.
func (c *Client) RunDiscard(ctx context.Context, args ...string) error {
	return c.R.RunDiscard(ctx, c.Cmd(args...))
}

// Output captures stdout.
func (c *Client) Output(ctx context.Context, args ...string) (string, error) {
	return c.R.Output(ctx, c.Cmd(args...))
}

// OutputCombined captures stdout+stderr.
func (c *Client) OutputCombined(ctx context.Context, args ...string) (string, error) {
	return c.R.OutputCombined(ctx, c.Cmd(args...))
}

// Healthz reports whether the cluster API is reachable.
func (c *Client) Healthz(ctx context.Context) bool {
	_, err := c.R.Output(ctx, c.Cmd("get", "--raw", "/healthz"))
	return err == nil
}

// ContextExists reports whether a kubectl context is present.
func ContextExists(ctx context.Context, r *execx.Runner, kubeconfig, name string) bool {
	args := []string{}
	if kubeconfig != "" {
		args = append(args, "--kubeconfig", kubeconfig)
	}
	args = append(args, "config", "get-contexts", "-o", "name")
	// A non-zero exit must not be fatal here.
	out, err := r.OutputCombined(ctx, execx.Cmd{Name: "kubectl", Args: args})
	if err != nil {
		return false
	}
	for _, line := range strings.Split(out, "\n") {
		if strings.TrimSpace(line) == name {
			return true
		}
	}
	return false
}

// CurrentContext returns the active kubectl context ("" when unset).
func CurrentContext(ctx context.Context, r *execx.Runner, kubeconfig string) string {
	args := []string{}
	if kubeconfig != "" {
		args = append(args, "--kubeconfig", kubeconfig)
	}
	args = append(args, "config", "current-context")
	out, err := r.OutputCombined(ctx, execx.Cmd{Name: "kubectl", Args: args})
	if err != nil {
		return ""
	}
	return strings.TrimSpace(out)
}

// UseContext selects a kubectl context.
func UseContext(ctx context.Context, r *execx.Runner, kubeconfig, name string) error {
	args := []string{}
	if kubeconfig != "" {
		args = append(args, "--kubeconfig", kubeconfig)
	}
	args = append(args, "config", "use-context", name)
	return r.RunDiscard(ctx, execx.Cmd{Name: "kubectl", Args: args})
}

// DeleteContext removes a kubectl context, ignoring absence.
func DeleteContext(ctx context.Context, r *execx.Runner, kubeconfig, name string) error {
	args := []string{}
	if kubeconfig != "" {
		args = append(args, "--kubeconfig", kubeconfig)
	}
	args = append(args, "config", "delete-context", name)
	_ = r.RunDiscard(ctx, execx.Cmd{Name: "kubectl", Args: args})
	return nil
}

// KubeContextMatches reports whether the current context equals want.
func ContextMatches(ctx context.Context, r *execx.Runner, kubeconfig, want string) bool {
	return CurrentContext(ctx, r, kubeconfig) == want
}

// GetJSON reads a resource as JSON into out.
func (c *Client) GetJSON(ctx context.Context, out any, args ...string) error {
	full := append([]string{"get"}, args...)
	full = append(full, "-o", "json")
	data, err := c.R.OutputBytes(ctx, c.Cmd(full...))
	if err != nil {
		return err
	}
	if err := json.Unmarshal(data, out); err != nil {
		return fmt.Errorf("decoding kubectl json: %w", err)
	}
	return nil
}

// ApplyJSON applies a YAML/JSON manifest from stdin via `kubectl apply -f -`.
func (c *Client) ApplyJSON(ctx context.Context, manifest string) error {
	cmd := c.Cmd("apply", "-f", "-")
	cmd.Stdin = strings.NewReader(manifest)
	cmd.Stdout = nil
	cmd.Stderr = nil
	return c.R.RunDiscard(ctx, cmd)
}

// DryRunClient renders a manifest server-side-less (client dry-run) and returns
// it, mirroring `kubectl ... --dry-run=client -o yaml`.
func (c *Client) DryRunClient(ctx context.Context, args ...string) (string, error) {
	full := append([]string{}, args...)
	full = append(full, "--dry-run=client", "-o", "yaml")
	return c.R.Output(ctx, c.Cmd(full...))
}
