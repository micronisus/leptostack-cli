package forge

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/micronisus/leptostack-cli/internal/execx"
)

// github provisions repositories via the gh CLI. GitHub does not expose an API
// to create personal access tokens, so the push/pull and read credentials reuse
// the token the gh CLI is already authenticated with (which must carry the
// "repo" scope).
type github struct {
	r   *execx.Runner
	opt Options
}

func (g *github) Server() Server { return GitHub }
func (g *github) CLI() string    { return "gh" }

func (g *github) EnsureCLI() error {
	if _, ok := execx.LookPath("gh"); !ok {
		return missingCLI("gh", GitHub)
	}
	return nil
}

func (g *github) Login(ctx context.Context) (string, error) {
	out, err := g.r.Output(ctx, execx.Cmd{Name: "gh", Args: []string{"api", "user", "--jq", ".login"}})
	if err != nil {
		return "", fmt.Errorf("could not determine the GitHub account (is `gh auth login` complete?): %w", err)
	}
	return strings.TrimSpace(out), nil
}

func (g *github) RepoExists(ctx context.Context, owner, name string) (bool, error) {
	out, err := g.r.OutputCombined(ctx, execx.Cmd{Name: "gh", Args: []string{"api", "repos/" + owner + "/" + name}})
	if err != nil {
		if strings.Contains(out, "404") || strings.Contains(strings.ToLower(out), "not found") {
			return false, nil
		}
		return false, fmt.Errorf("checking repository %s/%s: %w", owner, name, err)
	}
	return true, nil
}

func (g *github) CreateRepo(ctx context.Context, owner, name string, private bool) (Repo, error) {
	privateFlag := "true"
	if !private {
		privateFlag = "false"
	}
	out, err := g.r.Output(ctx, execx.Cmd{Name: "gh", Args: []string{
		"api", "--method", "POST", "user/repos",
		"-f", "name=" + name,
		"-F", "private=" + privateFlag,
		"-F", "auto_init=false",
	}})
	if err != nil {
		return Repo{}, fmt.Errorf("creating GitHub repository %s: %w", name, err)
	}
	var resp struct {
		FullName string `json:"full_name"`
		CloneURL string `json:"clone_url"`
	}
	if err := json.Unmarshal([]byte(out), &resp); err != nil {
		return Repo{}, fmt.Errorf("parsing gh response: %w", err)
	}
	return Repo{Owner: owner, Name: name, URL: resp.CloneURL}, nil
}

func (g *github) CreateRepoToken(ctx context.Context, repo Repo, name string) (Credential, error) {
	return g.existingToken(ctx)
}

func (g *github) CreateReadToken(ctx context.Context, owner string, repos []Repo, name string) (Credential, error) {
	cred, err := g.existingToken(ctx)
	if err != nil {
		return Credential{}, err
	}
	cred.Name = name
	cred.Note += " (GitHub does not support read-only PATs, so the same token is used for the template/base repositories)"
	return cred, nil
}

func (g *github) existingToken(ctx context.Context) (Credential, error) {
	tok, err := g.r.Output(ctx, execx.Cmd{Name: "gh", Args: []string{"auth", "token"}})
	tok = strings.TrimSpace(tok)
	if err != nil || tok == "" {
		return Credential{}, fmt.Errorf("could not read the gh token (run `gh auth login` with the 'repo' scope first)")
	}
	return Credential{
		Name:   "gh-auth-token",
		Token:  tok,
		Minted: false,
		Note:   "GitHub cannot create personal access tokens non-interactively; reusing the logged-in gh token",
	}, nil
}
