package forge

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/micronisus/leptostack-cli/internal/execx"
)

// forgejo provisions repositories via the fj CLI. fj cannot create access
// tokens, so token minting uses the Forgejo REST API with a management token
// supplied via --forge-token-stdin or LEPTOSTACK_FORGE_TOKEN.
type forgejo struct {
	r    *execx.Runner
	opt  Options
	host string

	// apiBaseURL overrides the derived API base (used by tests).
	apiBaseURL string
	// httpClient overrides the default client (used by tests).
	httpClient *http.Client
}

func (f *forgejo) Server() Server { return Forgejo }
func (f *forgejo) CLI() string    { return "fj" }

func (f *forgejo) EnsureCLI() error {
	if _, ok := execx.LookPath("fj"); !ok {
		return missingCLI("fj", Forgejo)
	}
	return nil
}

func (f *forgejo) Login(ctx context.Context) (string, error) {
	out, err := f.r.OutputCombined(ctx, execx.Cmd{Name: "fj", Args: []string{"whoami"}})
	if err != nil {
		return "", fmt.Errorf("could not determine the Forgejo account (is `fj auth login` complete?): %w", err)
	}
	// whoami output varies; take the last field of the last non-empty line.
	lines := strings.Split(strings.TrimSpace(out), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		fields := strings.Fields(strings.TrimSpace(lines[i]))
		if len(fields) > 0 {
			return fields[len(fields)-1], nil
		}
	}
	return "", fmt.Errorf("fj whoami returned no account name")
}

func (f *forgejo) RepoExists(ctx context.Context, owner, name string) (bool, error) {
	out, err := f.r.OutputCombined(ctx, execx.Cmd{Name: "fj", Args: f.hostFlag([]string{"repo", "view", owner + "/" + name})})
	if err != nil {
		low := strings.ToLower(out)
		if strings.Contains(out, "404") || strings.Contains(low, "not found") {
			return false, nil
		}
		return false, fmt.Errorf("checking repository %s/%s: %w", owner, name, err)
	}
	return true, nil
}

func (f *forgejo) CreateRepo(ctx context.Context, owner, name string, private bool) (Repo, error) {
	args := []string{"repo", "create", owner + "/" + name}
	if private {
		args = append(args, "-P")
	}
	if err := f.r.Run(ctx, execx.Cmd{Name: "fj", Args: f.hostFlag(args)}); err != nil {
		return Repo{}, fmt.Errorf("creating Forgejo repository %s/%s: %w", owner, name, err)
	}
	return Repo{Owner: owner, Name: name, URL: f.webURL(owner, name)}, nil
}

func (f *forgejo) CreateRepoToken(ctx context.Context, repo Repo, name string) (Credential, error) {
	tok, err := f.mintToken(ctx, repo.Owner, name, []string{"read:repository", "write:repository"})
	if err != nil {
		return Credential{}, err
	}
	return Credential{Name: name, Token: tok, Minted: true}, nil
}

func (f *forgejo) CreateReadToken(ctx context.Context, owner string, repos []Repo, name string) (Credential, error) {
	if len(repos) == 0 {
		return Credential{}, fmt.Errorf("no repositories supplied for the read token")
	}
	// A user-scoped read:repository token covers every repository the user can
	// read, so mint it for the authenticated account, not the base owner.
	tok, err := f.mintToken(ctx, owner, name, []string{"read:repository"})
	if err != nil {
		return Credential{}, err
	}
	return Credential{Name: name, Token: tok, Minted: true}, nil
}

func (f *forgejo) hostFlag(args []string) []string {
	if f.host == "" {
		return args
	}
	return append([]string{"-H", f.host}, args...)
}

func (f *forgejo) webURL(owner, name string) string {
	host := f.host
	if host == "" {
		host = "localhost"
	}
	return fmt.Sprintf("https://%s/%s/%s.git", host, owner, name)
}

func (f *forgejo) apiBase() string {
	if f.apiBaseURL != "" {
		return strings.TrimRight(f.apiBaseURL, "/")
	}
	return "https://" + f.host
}

func (f *forgejo) client() *http.Client {
	if f.httpClient != nil {
		return f.httpClient
	}
	return http.DefaultClient
}

// mintToken creates a scoped access token via POST /api/v1/users/{user}/tokens.
func (f *forgejo) mintToken(ctx context.Context, owner, name string, scopes []string) (string, error) {
	if strings.TrimSpace(f.opt.ForgeToken) == "" {
		return "", fmt.Errorf("creating Forgejo tokens needs a management token; pass --forge-token-stdin or set LEPTOSTACK_FORGE_TOKEN (fj cannot mint tokens)")
	}
	if f.host == "" {
		return "", fmt.Errorf("the Forgejo hostname is required to create tokens (set --hostname)")
	}
	payload, err := json.Marshal(map[string]any{"name": name, "scopes": scopes})
	if err != nil {
		return "", err
	}
	url := f.apiBase() + "/api/v1/users/" + owner + "/tokens"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "token "+f.opt.ForgeToken)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	resp, err := f.client().Do(req)
	if err != nil {
		return "", fmt.Errorf("creating Forgejo token for %s: %w", owner, err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		return "", fmt.Errorf("creating Forgejo token for %s: HTTP %d: %s", owner, resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var out struct {
		Token string `json:"token"`
		SHA1  string `json:"sha1"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return "", fmt.Errorf("parsing Forgejo token response: %w", err)
	}
	tok := out.Token
	if tok == "" {
		tok = out.SHA1
	}
	if tok == "" {
		return "", fmt.Errorf("Forgejo did not return a token; is the management token allowed to create tokens for %q?", owner)
	}
	return tok, nil
}
