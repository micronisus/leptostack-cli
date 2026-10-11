// Package gitops ports the three git-heavy workflows from the bash
// implementation: cluster-template sync, Flux bootstrap, and FluxCD base
// manifest refresh. It keeps the GitOps repository as the source of truth.
package gitops

import (
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/micronisus/leptostack-cli/internal/cli"
	"github.com/micronisus/leptostack-cli/internal/config"
	"github.com/micronisus/leptostack-cli/internal/execx"
	"github.com/micronisus/leptostack-cli/internal/flux"
)

// FluxTemplatePaths are the template paths refreshed by `sync flux`.
var FluxTemplatePaths = []string{
	"apps/devops/base",
	"apps/leptostack/base",
	"infrastructure/leptostack-infra.yaml",
}

// Manager runs GitOps workflows for a resolved cluster.
type Manager struct {
	R        *execx.Runner
	IO       *cli.IOStreams
	Resolved *config.Resolved
	Flux     *flux.Client
}

// New builds a Manager.
func New(r *execx.Runner, io *cli.IOStreams, resolved *config.Resolved, f *flux.Client) *Manager {
	return &Manager{R: r, IO: io, Resolved: resolved, Flux: f}
}

func (m *Manager) gitHost() string {
	g := m.Resolved.Git
	if g.Hostname != "" {
		return g.Hostname
	}
	switch g.Server {
	case config.ServerGitea:
		return g.Hostname
	case config.ServerGitLab:
		return "gitlab.com"
	default:
		return "github.com"
	}
}

// repoURL builds the clone URL for the GitOps repository. Credentials are
// passed out-of-band through the git environment (see gitAuthEnv) so the PAT
// never appears in the process argv.
func (m *Manager) repoURL() string {
	g := m.Resolved.Git
	return fmt.Sprintf("https://%s/%s/%s.git", m.gitHost(), g.Owner, g.Repository)
}

// templateURL builds the (credential-free) clone URL for the template repo.
func (m *Manager) templateURL() string {
	raw := m.Resolved.Template.URL
	raw = strings.TrimPrefix(raw, "https://")
	raw = strings.TrimPrefix(raw, "http://")
	return "https://" + raw
}

func (m *Manager) templatePAT() string {
	if m.Resolved.Template.PAT != "" {
		return m.Resolved.Template.PAT
	}
	return m.Resolved.Git.PAT
}

// gitAuthEnv returns git environment variables that carry an HTTP Basic
// Authorization header without placing the credential in argv.
func gitAuthEnv(pat string) map[string]string {
	if pat == "" {
		return nil
	}
	basic := base64.StdEncoding.EncodeToString([]byte("oauth2:" + pat))
	return map[string]string{
		"GIT_CONFIG_COUNT":   "1",
		"GIT_CONFIG_KEY_0":   "http.extraHeader",
		"GIT_CONFIG_VALUE_0": "Authorization: Basic " + basic,
	}
}

func (m *Manager) gitAs(ctx context.Context, pat, dir string, args ...string) error {
	cmd := execx.Cmd{Name: "git", Args: args, Dir: dir, Env: gitAuthEnv(pat)}
	cmd.Stdout = m.IO.ErrOut
	cmd.Stderr = m.IO.ErrOut
	return m.R.Run(ctx, cmd)
}

func (m *Manager) git(ctx context.Context, dir string, args ...string) error {
	return m.gitAs(ctx, m.Resolved.Git.PAT, dir, args...)
}

func (m *Manager) gitOutput(ctx context.Context, dir string, args ...string) (string, error) {
	return m.R.OutputCombined(ctx, execx.Cmd{Name: "git", Args: args, Dir: dir, Env: gitAuthEnv(m.Resolved.Git.PAT)})
}

func (m *Manager) commitAndPush(ctx context.Context, dir, message, noChangesMessage string, extraPush ...string) error {
	_ = m.git(ctx, dir, "config", "user.name", "LeptoStack Bootstrap")
	_ = m.git(ctx, dir, "config", "user.email", "bootstrap@leptostack.local")
	if err := m.git(ctx, dir, "add", "-A"); err != nil {
		return err
	}
	out, err := m.gitOutput(ctx, dir, "diff", "--cached", "--quiet")
	if err == nil {
		m.IO.Errln(noChangesMessage)
		return nil
	}
	_ = out
	if err := m.git(ctx, dir, "commit", "-m", message); err != nil {
		return err
	}
	pushArgs := []string{"push", "origin"}
	if len(extraPush) > 0 {
		pushArgs = append(pushArgs, extraPush...)
	} else {
		pushArgs = append(pushArgs, m.Resolved.Git.Branch)
	}
	m.IO.Errln("Pushing changes...")
	if err := m.git(ctx, dir, pushArgs...); err != nil {
		return err
	}
	m.IO.Errln("Changes pushed successfully.")
	return nil
}

// SyncClusterTemplate populates the GitOps repository from the cluster
// template. force re-runs even when the cluster path already exists.
func (m *Manager) SyncClusterTemplate(ctx context.Context, force bool) error {
	g := m.Resolved.Git
	cloneURL := m.repoURL()

	tmp, err := os.MkdirTemp("", "leptostack-template-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)

	refs, _ := m.gitOutput(ctx, "", "ls-remote", cloneURL, "refs/heads/"+g.Branch)
	repoExists := strings.TrimSpace(refs) != ""

	repoDir := filepath.Join(tmp, "repo")
	m.IO.Errf("Cloning repository %s/%s...\n", g.Owner, g.Repository)
	if repoExists {
		if err := m.git(ctx, "", "clone", "--branch", g.Branch, "--depth", "1", cloneURL, repoDir); err != nil {
			return err
		}
		clusterPath := filepath.Join(repoDir, filepath.FromSlash(m.Resolved.ClonePath()))
		if !force && dirExists(clusterPath) {
			m.IO.Errf("Repository %s/%s already contains the cluster configuration for '%s'; skipping cluster template sync.\n", g.Owner, g.Repository, m.Resolved.ClusterName)
			return nil
		}
	} else {
		if err := m.git(ctx, "", "clone", cloneURL, repoDir); err != nil {
			return err
		}
		if err := m.git(ctx, repoDir, "checkout", "-B", g.Branch); err != nil {
			return err
		}
	}

	m.IO.Errf("Cloning cluster template %s...\n", m.Resolved.Template.URL)
	templateDir := filepath.Join(tmp, "template")
	if err := m.gitAs(ctx, m.templatePAT(), "", "clone", "--branch", m.Resolved.Template.Branch, "--depth", "1", m.templateURL(), templateDir); err != nil {
		return err
	}
	_ = m.git(ctx, templateDir, "remote", "remove", "origin")

	m.IO.Errln("Replacing repository contents with the cluster template...")
	if err := replaceContents(templateDir, repoDir); err != nil {
		return err
	}

	exampleDir := filepath.Join(repoDir, "clusters", "example")
	if !dirExists(exampleDir) {
		return fmt.Errorf("clusters/example not found in the cluster template")
	}
	if m.Resolved.ClonePath() == "clusters/example" {
		return fmt.Errorf("clusterPath cannot be clusters/example (reserved by the cluster template)")
	}
	clusterDir := filepath.Join(repoDir, filepath.FromSlash(m.Resolved.ClonePath()))
	if err := os.RemoveAll(clusterDir); err != nil {
		return err
	}
	if err := os.Rename(exampleDir, clusterDir); err != nil {
		return err
	}
	m.IO.Errf("Copied clusters/example to %s.\n", m.Resolved.ClonePath())

	clusterKust := filepath.Join(clusterDir, "kustomization.yaml")
	if !fileExists(clusterKust) {
		return fmt.Errorf("could not find %s/kustomization.yaml in the cluster template", m.Resolved.ClonePath())
	}
	if err := renameExampleReferences(clusterKust, m.Resolved.ClusterName); err != nil {
		return err
	}
	m.IO.Errf("Renamed example cluster references to %s.\n", m.Resolved.ClusterName)

	if changed, err := addInternalCADeletePatch(clusterKust); err != nil {
		return err
	} else if changed {
		m.IO.Errln("  Patched the infra-config Kustomization to delete the self-signed internal-ca Certificate.")
	}

	if err := m.generateClusterConfig(ctx, repoDir); err != nil {
		return err
	}

	m.IO.Errf("Cluster overlay files created at apps/leptostack/overlays/%s\n", m.Resolved.ClusterName)
	if repoExists {
		return m.commitAndPush(ctx, repoDir, "Sync cluster template from "+m.Resolved.Template.URL,
			"Repository is already in sync with the cluster template; no changes to push.")
	}
	return m.commitAndPush(ctx, repoDir, "Sync cluster template from "+m.Resolved.Template.URL,
		"Repository is already in sync with the cluster template; no changes to push.", "-u", "HEAD:"+g.Branch)
}

// RunBootstrap syncs the cluster template and runs flux bootstrap.
func (m *Manager) RunBootstrap(ctx context.Context, resync bool) error {
	if err := m.SyncClusterTemplate(ctx, resync); err != nil {
		return err
	}
	g := m.Resolved.Git
	m.IO.Errf("Running flux bootstrap for %s...\n\n", g.Server)
	if err := m.Flux.Bootstrap(ctx, g.Server, g.PAT, m.gitHost(), g.Owner, g.Repository, g.Branch, m.Resolved.ClonePath()); err != nil {
		return err
	}
	m.IO.Errln("\nFlux bootstrap has been started.\nYou can check the status of the deployment by running:\n  flux get kustomizations")
	return nil
}

// CommitFile writes a file into the GitOps repository copy and pushes it.
func (m *Manager) CommitFile(ctx context.Context, relPath, content, message string) error {
	tmp, err := os.MkdirTemp("", "leptostack-commit-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)

	repoDir := filepath.Join(tmp, "repo")
	m.IO.Errf("Cloning repository %s/%s (%s)...\n", m.Resolved.Git.Owner, m.Resolved.Git.Repository, m.Resolved.Git.Branch)
	if err := m.git(ctx, "", "clone", "--branch", m.Resolved.Git.Branch, "--depth", "1", m.repoURL(), repoDir); err != nil {
		return err
	}
	dst := filepath.Join(repoDir, filepath.FromSlash(relPath))
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(dst, []byte(content), 0o644); err != nil {
		return err
	}
	m.IO.Errf("Wrote %s.\n", relPath)
	return m.commitAndPush(ctx, repoDir, message, "No changes to push.")
}

// RemoveFile deletes a file from the GitOps repository copy and pushes it.
func (m *Manager) RemoveFile(ctx context.Context, relPath, message string) error {
	tmp, err := os.MkdirTemp("", "leptostack-remove-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)

	repoDir := filepath.Join(tmp, "repo")
	if err := m.git(ctx, "", "clone", "--branch", m.Resolved.Git.Branch, "--depth", "1", m.repoURL(), repoDir); err != nil {
		return err
	}
	dst := filepath.Join(repoDir, filepath.FromSlash(relPath))
	if err := os.Remove(dst); err != nil && !os.IsNotExist(err) {
		return err
	}
	m.IO.Errf("Removed %s from the repository.\n", relPath)
	return m.commitAndPush(ctx, repoDir, message, "No changes to push.")
}

// UpdateFlux refreshes the FluxCD base manifests from the cluster template.
func (m *Manager) UpdateFlux(ctx context.Context) error {
	g := m.Resolved.Git
	tmp, err := os.MkdirTemp("", "leptostack-update-flux-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)

	m.IO.Errf("Cloning FluxCD repository %s/%s (%s)...\n", g.Owner, g.Repository, g.Branch)
	fluxDir := filepath.Join(tmp, "fluxcd")
	if err := m.git(ctx, "", "clone", "--branch", g.Branch, "--depth", "1", m.repoURL(), fluxDir); err != nil {
		return fmt.Errorf("could not clone %s/%s at branch %q: %w", g.Owner, g.Repository, g.Branch, err)
	}

	m.IO.Errf("Cloning cluster template %s (%s)...\n", m.Resolved.Template.URL, m.Resolved.Template.Branch)
	templateDir := filepath.Join(tmp, "template")
	if err := m.gitAs(ctx, m.templatePAT(), "", "clone", "--branch", m.Resolved.Template.Branch, "--depth", "1", m.templateURL(), templateDir); err != nil {
		return err
	}
	_ = m.git(ctx, templateDir, "remote", "remove", "origin")

	changed := false
	for _, p := range FluxTemplatePaths {
		src := filepath.Join(templateDir, filepath.FromSlash(p))
		dst := filepath.Join(fluxDir, filepath.FromSlash(p))
		if !pathExists(src) {
			m.IO.Errf("  %s: not found in the cluster template; skipping.\n", p)
			continue
		}
		same, _ := dirsEqual(src, dst)
		if pathExists(dst) && same {
			m.IO.Errf("  %s: up to date.\n", p)
			continue
		}
		m.IO.Errf("  %s: changed, updating.\n", p)
		if err := os.RemoveAll(dst); err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return err
		}
		if err := copyPath(src, dst); err != nil {
			return err
		}
		changed = true
	}

	if !changed {
		m.IO.Errln("FluxCD repository is already up to date with the cluster template.")
		return nil
	}
	return m.commitAndPush(ctx, fluxDir, "Update FluxCD manifests from cluster template",
		"FluxCD repository is already up to date with the cluster template.")
}
