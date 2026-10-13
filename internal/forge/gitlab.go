package forge

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/micronisus/leptostack-cli/internal/execx"
)

// gitlab provisions repositories and access tokens via the glab CLI. Project
// access tokens are used for the new repository (push/pull) and a group access
// token (or per-project tokens) for the read-only base repository.
type gitlab struct {
	r    *execx.Runner
	opt  Options
	host string
}

func (g *gitlab) Server() Server { return GitLab }
func (g *gitlab) CLI() string    { return "glab" }

func (g *gitlab) EnsureCLI() error {
	if _, ok := execx.LookPath("glab"); !ok {
		return missingCLI("glab", GitLab)
	}
	return nil
}

func (g *gitlab) Login(ctx context.Context) (string, error) {
	out, err := g.r.Output(ctx, g.cmd("api", "user"))
	if err != nil {
		return "", fmt.Errorf("could not determine the GitLab account (is `glab auth login` complete?): %w", err)
	}
	var user struct {
		Username string `json:"username"`
	}
	if err := json.Unmarshal([]byte(out), &user); err != nil {
		return "", fmt.Errorf("parsing GitLab user response: %w", err)
	}
	if user.Username == "" {
		return "", fmt.Errorf("GitLab did not return a username")
	}
	return user.Username, nil
}

func (g *gitlab) RepoExists(ctx context.Context, owner, name string) (bool, error) {
	out, err := g.r.OutputCombined(ctx, g.cmd("repo", "view", owner+"/"+name))
	if err != nil {
		low := strings.ToLower(out)
		if strings.Contains(out, "404") || strings.Contains(low, "not found") {
			return false, nil
		}
		return false, fmt.Errorf("checking repository %s/%s: %w", owner, name, err)
	}
	return true, nil
}

func (g *gitlab) CreateRepo(ctx context.Context, owner, name string, private bool) (Repo, error) {
	visibility := "private"
	if !private {
		visibility = "public"
	}
	out, err := g.r.Output(ctx, g.cmd("api", "--method", "POST", "projects", "-f", "name="+name, "-f", "visibility="+visibility))
	if err != nil {
		return Repo{}, fmt.Errorf("creating GitLab project %s: %w", name, err)
	}
	var resp struct {
		PathWithNamespace string `json:"path_with_namespace"`
		HTTPURLToRepo     string `json:"http_url_to_repo"`
	}
	if err := json.Unmarshal([]byte(out), &resp); err != nil {
		return Repo{}, fmt.Errorf("parsing glab response: %w", err)
	}
	full := resp.PathWithNamespace
	if full == "" {
		full = owner + "/" + name
	}
	return Repo{Owner: owner, Name: name, URL: resp.HTTPURLToRepo}, nil
}

func (g *gitlab) CreateRepoToken(ctx context.Context, repo Repo, name string) (Credential, error) {
	tok, err := g.createToken(ctx, name, []string{"--repo", repo.FullName(), "--access-level", "developer", "--scope", "write_repository"})
	if err != nil {
		return Credential{}, err
	}
	return Credential{Name: name, Token: tok, Minted: true}, nil
}

func (g *gitlab) CreateReadToken(ctx context.Context, owner string, repos []Repo, name string) (Credential, error) {
	if len(repos) == 0 {
		return Credential{}, fmt.Errorf("no repositories supplied for the read token")
	}
	// A single group token covers every repository when they share a namespace.
	shared := true
	for _, r := range repos {
		if r.Owner != repos[0].Owner {
			shared = false
			break
		}
	}
	if shared {
		tok, err := g.createToken(ctx, name, []string{"--group", repos[0].Owner, "--access-level", "reporter", "--scope", "read_repository"})
		if err != nil {
			return Credential{}, err
		}
		return Credential{Name: name, Token: tok, Minted: true}, nil
	}
	// Otherwise create per-project read tokens and use the first; list the rest.
	var first Credential
	var names []string
	for i, r := range repos {
		tok, err := g.createToken(ctx, fmt.Sprintf("%s-%d", name, i+1), []string{"--repo", r.FullName(), "--access-level", "reporter", "--scope", "read_repository"})
		if err != nil {
			return Credential{}, err
		}
		names = append(names, r.FullName())
		if i == 0 {
			first = Credential{Name: name, Token: tok, Minted: true}
		}
	}
	first.Note = fmt.Sprintf("the repositories %s do not share a namespace; separate read tokens were created and the first is stored", strings.Join(names, ", "))
	return first, nil
}

func (g *gitlab) createToken(ctx context.Context, name string, scopeArgs []string) (string, error) {
	args := []string{"token", "create", name}
	args = append(args, scopeArgs...)
	args = append(args, "--duration", fmt.Sprintf("%dd", g.opt.tokenDays()))
	out, err := g.r.Output(ctx, g.cmd(args...))
	if err != nil {
		return "", fmt.Errorf("creating GitLab token %s: %w", name, err)
	}
	tok := extractGitLabToken(out)
	if tok == "" {
		return "", fmt.Errorf("glab did not return a token for %s", name)
	}
	return tok, nil
}

// cmd builds a glab invocation, selecting the configured host via GITLAB_HOST.
func (g *gitlab) cmd(args ...string) execx.Cmd {
	c := execx.Cmd{Name: "glab", Args: args}
	if g.host != "" {
		c.Env = map[string]string{"GITLAB_HOST": g.host}
	}
	return c
}

var gitlabTokenRe = regexp.MustCompile(`glpat-[A-Za-z0-9_\-]{10,}`)

func extractGitLabToken(out string) string {
	if m := gitlabTokenRe.FindString(out); m != "" {
		return m
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if s := strings.TrimSpace(lines[i]); s != "" {
			return s
		}
	}
	return ""
}
