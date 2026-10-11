// Package flux wraps the flux CLI for reconciliation, bootstrap and
// suspend/resume actions. Structured reads use kubectl JSON via the kubernetes
// package because `flux get` emits tables, not JSON.
package flux

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/micronisus/leptostack-cli/internal/execx"
)

// Client runs flux with a fixed context and kubeconfig.
type Client struct {
	R           *execx.Runner
	KubeContext string
	Kubeconfig  string
}

// New builds a Client.
func New(r *execx.Runner, kubeContext, kubeconfig string) *Client {
	return &Client{R: r, KubeContext: kubeContext, Kubeconfig: kubeconfig}
}

// Cmd builds an execx.Cmd for flux with global context/kubeconfig prepended.
func (c *Client) Cmd(args ...string) execx.Cmd {
	full := []string{}
	if c.Kubeconfig != "" {
		full = append(full, "--kubeconfig", c.Kubeconfig)
	}
	if c.KubeContext != "" {
		full = append(full, "--context", c.KubeContext)
	}
	full = append(full, args...)
	return execx.Cmd{Name: "flux", Args: full}
}

// Run streams a flux command.
func (c *Client) Run(ctx context.Context, args ...string) error {
	return c.R.Run(ctx, c.Cmd(args...))
}

// Version returns the flux version string.
func (c *Client) Version(ctx context.Context) (string, error) {
	out, err := c.R.OutputCombined(ctx, c.Cmd("version"))
	if err != nil {
		return "", err
	}
	return firstNonEmptyLine(out), nil
}

// ClientVersion returns `flux --version`-style version, parsed from `flux -v`.
func (c *Client) ClientVersion(ctx context.Context) (string, error) {
	out, err := c.R.OutputCombined(ctx, c.Cmd("-v"))
	if err != nil {
		return "", err
	}
	return extractSemver(out), nil
}

// GetKustomizationsHuman returns the human `flux get kustomizations` table.
func (c *Client) GetKustomizationsHuman(ctx context.Context) (string, error) {
	return c.R.OutputCombined(ctx, c.Cmd("get", "kustomizations"))
}

// ReconcileKustomization triggers reconciliation of a kustomization.
func (c *Client) ReconcileKustomization(ctx context.Context, name string, withSource bool, timeout time.Duration) error {
	args := []string{"reconcile", "kustomization", name}
	if withSource {
		args = append(args, "--with-source")
	}
	if timeout > 0 {
		args = append(args, "--timeout", timeout.String())
	}
	return c.Run(ctx, args...)
}

// ReconcileSource triggers reconciliation of a source of the given kind.
func (c *Client) ReconcileSource(ctx context.Context, kind, name string, timeout time.Duration) error {
	args := []string{"reconcile", "source", kind, name}
	if timeout > 0 {
		args = append(args, "--timeout", timeout.String())
	}
	return c.Run(ctx, args...)
}

// Suspend suspends reconciliation of a kustomization.
func (c *Client) Suspend(ctx context.Context, name string) error {
	return c.Run(ctx, "suspend", "kustomization", name)
}

// Resume resumes reconciliation of a kustomization.
func (c *Client) Resume(ctx context.Context, name string) error {
	return c.Run(ctx, "resume", "kustomization", name)
}

// CreateSourceArgs returns the `flux create source ...` argv for a kind.
func (c *Client) CreateSource(ctx context.Context, args ...string) error {
	return c.Run(ctx, append([]string{"create", "source"}, args...)...)
}

// Bootstrap runs flux bootstrap for the configured server, exporting the PAT in
// the provider-specific environment variable.
func (c *Client) Bootstrap(ctx context.Context, server, token, hostname, owner, repo, branch, path string) error {
	args := []string{"bootstrap"}
	switch server {
	case "github":
		args = append(args, "github", "--token-auth", "--personal")
	case "gitea":
		args = append(args, "gitea", "--token-auth", "--personal")
		if hostname != "" {
			args = append(args, "--hostname="+hostname)
		}
	case "gitlab":
		args = append(args, "gitlab", "--token-auth")
		if hostname != "" {
			args = append(args, "--hostname="+hostname)
		}
	default:
		return fmt.Errorf("unsupported git server %q", server)
	}
	args = append(args,
		"--components-extra=image-reflector-controller,image-automation-controller",
		"--owner="+owner,
		"--repository="+repo,
		"--branch="+branch,
		"--path="+path,
	)
	cmd := c.Cmd(args...)
	env := map[string]string{}
	switch server {
	case "github":
		env["GITHUB_TOKEN"] = token
	case "gitea":
		env["GITEA_TOKEN"] = token
	case "gitlab":
		env["GITLAB_TOKEN"] = token
	}
	cmd.Env = env
	return c.R.Run(ctx, cmd)
}

func firstNonEmptyLine(s string) string {
	for _, line := range strings.Split(s, "\n") {
		if strings.TrimSpace(line) != "" {
			return strings.TrimSpace(line)
		}
	}
	return ""
}

func extractSemver(s string) string {
	for _, f := range strings.Fields(s) {
		f = strings.TrimPrefix(f, "v")
		if looksSemver(f) {
			return f
		}
	}
	return strings.TrimSpace(s)
}

func looksSemver(s string) bool {
	parts := strings.Split(s, ".")
	if len(parts) != 3 {
		return false
	}
	for _, p := range parts {
		if p == "" {
			return false
		}
		for _, r := range p {
			if r < '0' || r > '9' {
				return false
			}
		}
	}
	return true
}
