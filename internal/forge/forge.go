// Package forge automates GitOps repository provisioning (Phase 2 of cluster
// creation): verifying the required forge CLI, creating a uniquely named
// repository under the user's personal account without ever replacing an
// existing one, and minting the push/pull and read-only credentials the FluxCD
// bootstrap needs.
//
// The three hosts are handled by their CLIs:
//
//	github -> gh    (repo creation + logged-in token; GitHub cannot mint PATs)
//	gitea  -> fj    (Forgejo/Gitea repo creation; token minting via the API)
//	gitlab -> glab  (repo creation + project/group access tokens)
package forge

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/url"
	"strings"

	"github.com/micronisus/leptostack-cli/internal/cli"
	"github.com/micronisus/leptostack-cli/internal/execx"
)

// Server identifies the Git host family.
type Server string

// Supported servers. Forgejo is configured under the "gitea" server id.
const (
	GitHub  Server = "github"
	Forgejo Server = "gitea"
	GitLab  Server = "gitlab"
)

// ParseServer validates a server identifier.
func ParseServer(s string) (Server, error) {
	switch Server(strings.ToLower(strings.TrimSpace(s))) {
	case GitHub:
		return GitHub, nil
	case Forgejo, "forgejo":
		return Forgejo, nil
	case GitLab:
		return GitLab, nil
	}
	return "", fmt.Errorf("unsupported git server %q (expected github, gitea or gitlab)", s)
}

// Repo identifies a repository.
type Repo struct {
	Owner string
	Name  string
	URL   string
}

// FullName returns owner/name.
func (r Repo) FullName() string { return r.Owner + "/" + r.Name }

// Credential is a minted or reused access token.
type Credential struct {
	Name   string
	Token  string
	Minted bool   // true when the forge created a new token
	Note   string // human explanation, e.g. why an existing token was reused
}

// Options configures the forge client.
type Options struct {
	Hostname   string // forge host (gitea/gitlab); used for URLs and API calls
	ForgeToken string // management token for API-based minting (Forgejo/Gitea)
	TokenDays  int    // requested token lifetime in days
	Private    bool   // create the repository private
}

// Client performs Phase 2 operations for one forge.
type Client interface {
	// Server returns the server identifier.
	Server() Server
	// CLI returns the required CLI binary name (gh/fj/glab).
	CLI() string
	// EnsureCLI verifies the CLI is installed, failing with exit code 4.
	EnsureCLI() error
	// Login returns the authenticated user's login/username.
	Login(ctx context.Context) (string, error)
	// RepoExists reports whether owner/name already exists.
	RepoExists(ctx context.Context, owner, name string) (bool, error)
	// CreateRepo creates owner/name (never replacing an existing repository).
	CreateRepo(ctx context.Context, owner, name string, private bool) (Repo, error)
	// CreateRepoToken mints a push/pull credential for repo.
	CreateRepoToken(ctx context.Context, repo Repo, name string) (Credential, error)
	// CreateReadToken mints a read-only (pull) credential for owner covering
	// repos. owner is the authenticated personal account.
	CreateReadToken(ctx context.Context, owner string, repos []Repo, name string) (Credential, error)
}

// New builds a client for the server.
func New(server Server, r *execx.Runner, opt Options) (Client, error) {
	switch server {
	case GitHub:
		return &github{r: r, opt: opt}, nil
	case Forgejo:
		return &forgejo{r: r, host: opt.Hostname, opt: opt}, nil
	case GitLab:
		return &gitlab{r: r, host: opt.Hostname, opt: opt}, nil
	}
	return nil, fmt.Errorf("unsupported git server %q", server)
}

func missingCLI(cliName string, server Server) error {
	return cli.Exitf(cli.ExitPrereq,
		"the %q CLI is required to automate repository creation for %s but was not found on PATH", cliName, server)
}

// SplitRepo parses "owner/name" (optionally a full https URL or with .git).
func SplitRepo(s string) (owner, name string, err error) {
	s = strings.TrimSpace(s)
	if strings.Contains(s, "://") {
		u, perr := url.Parse(s)
		if perr != nil {
			return "", "", fmt.Errorf("invalid repository reference %q: %w", s, perr)
		}
		s = strings.TrimPrefix(u.Path, "/")
	}
	s = strings.TrimSuffix(s, ".git")
	parts := strings.Split(s, "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", fmt.Errorf("invalid repository reference %q; expected owner/name", s)
	}
	return parts[0], parts[1], nil
}

// RepoFromURL derives a Repo (owner/name) from a clone URL.
func RepoFromURL(raw string) (Repo, error) {
	owner, name, err := SplitRepo(raw)
	if err != nil {
		return Repo{}, err
	}
	return Repo{Owner: owner, Name: name}, nil
}

// UniqueRepoName returns base if it is free, otherwise base-<suffix> with a
// random suffix, probing the forge so an existing repository is never replaced.
func UniqueRepoName(ctx context.Context, c Client, owner, base string) (string, error) {
	exists, err := c.RepoExists(ctx, owner, base)
	if err != nil {
		return "", err
	}
	if !exists {
		return base, nil
	}
	for i := 0; i < 6; i++ {
		candidate := base + "-" + randomSuffix(6)
		exists, err := c.RepoExists(ctx, owner, candidate)
		if err != nil {
			return "", err
		}
		if !exists {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("could not find an unused repository name based on %q under %s", base, owner)
}

func randomSuffix(n int) string {
	b := make([]byte, (n+1)/2)
	if _, err := rand.Read(b); err != nil {
		return "000000"[:n]
	}
	return hex.EncodeToString(b)[:n]
}

// tokenDays returns the configured lifetime or the default (365 days).
func (o Options) tokenDays() int {
	if o.TokenDays > 0 {
		return o.TokenDays
	}
	return 365
}
