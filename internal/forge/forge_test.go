package forge

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestParseServer(t *testing.T) {
	for in, want := range map[string]Server{
		"github":  GitHub,
		"gitea":   Forgejo,
		"forgejo": Forgejo,
		"gitlab":  GitLab,
	} {
		got, err := ParseServer(in)
		if err != nil || got != want {
			t.Fatalf("ParseServer(%q)=%q,%v want %q", in, got, err, want)
		}
	}
	if _, err := ParseServer("bitbucket"); err == nil {
		t.Fatal("expected error for unsupported server")
	}
}

func TestSplitRepo(t *testing.T) {
	cases := map[string][2]string{
		"owner/name":                                    {"owner", "name"},
		"https://github.com/owner/name.git":             {"owner", "name"},
		"https://gitlab.example.com/group/sub/proj.git": {}, // 3 segments -> error
	}
	for in, want := range cases {
		owner, name, err := SplitRepo(in)
		if want == ([2]string{}) {
			if err == nil {
				t.Errorf("SplitRepo(%q) expected error", in)
			}
			continue
		}
		if err != nil || owner != want[0] || name != want[1] {
			t.Errorf("SplitRepo(%q)=%q,%q,%v want %v", in, owner, name, err, want)
		}
	}
}

type fakeClient struct {
	existing map[string]bool
}

func (f *fakeClient) Server() Server                        { return GitHub }
func (f *fakeClient) CLI() string                           { return "fake" }
func (f *fakeClient) EnsureCLI() error                      { return nil }
func (f *fakeClient) Login(context.Context) (string, error) { return "alice", nil }
func (f *fakeClient) RepoExists(_ context.Context, _, name string) (bool, error) {
	return f.existing[name], nil
}
func (f *fakeClient) CreateRepo(context.Context, string, string, bool) (Repo, error) {
	return Repo{}, nil
}
func (f *fakeClient) CreateRepoToken(context.Context, Repo, string) (Credential, error) {
	return Credential{}, nil
}
func (f *fakeClient) CreateReadToken(context.Context, string, []Repo, string) (Credential, error) {
	return Credential{}, nil
}

func TestUniqueRepoName(t *testing.T) {
	c := &fakeClient{existing: map[string]bool{"local-gitops": true}}
	got, err := UniqueRepoName(context.Background(), c, "alice", "local-gitops")
	if err != nil {
		t.Fatal(err)
	}
	if got == "local-gitops" || !strings.HasPrefix(got, "local-gitops-") {
		t.Fatalf("expected a suffixed unique name, got %q", got)
	}
	// Free name is returned unchanged.
	c2 := &fakeClient{existing: map[string]bool{}}
	if got, _ := UniqueRepoName(context.Background(), c2, "alice", "fresh"); got != "fresh" {
		t.Fatalf("expected \"fresh\", got %q", got)
	}
}

func TestForgejoMintToken(t *testing.T) {
	var gotPath, gotAuth string
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &gotBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"token":"tok123"}`))
	}))
	defer srv.Close()

	f := &forgejo{
		host:       "forgejo.test",
		opt:        Options{ForgeToken: "mgmt-token", TokenDays: 30},
		apiBaseURL: srv.URL,
		httpClient: srv.Client(),
	}
	cred, err := f.CreateReadToken(context.Background(), "alice", []Repo{{Owner: "alice", Name: "cluster-template"}}, "read")
	if err != nil {
		t.Fatal(err)
	}
	if cred.Token != "tok123" || !cred.Minted {
		t.Fatalf("credential = %+v", cred)
	}
	if gotPath != "/api/v1/users/alice/tokens" {
		t.Errorf("path = %q", gotPath)
	}
	if gotAuth != "token mgmt-token" {
		t.Errorf("auth = %q", gotAuth)
	}
	scopes, _ := gotBody["scopes"].([]any)
	if len(scopes) != 1 || scopes[0] != "read:repository" {
		t.Errorf("scopes = %v", gotBody["scopes"])
	}
}

func TestForgejoMintRequiresToken(t *testing.T) {
	f := &forgejo{host: "forgejo.test", opt: Options{}}
	if _, err := f.CreateRepoToken(context.Background(), Repo{Owner: "alice", Name: "x"}, "n"); err == nil {
		t.Fatal("expected an error when no management token is configured")
	}
}

func TestGitLabExtractToken(t *testing.T) {
	if got := extractGitLabToken("creating token...\nglpat-abcdef1234567890\n"); got != "glpat-abcdef1234567890" {
		t.Fatalf("extract = %q", got)
	}
	if got := extractGitLabToken("plainvalue\n"); got != "plainvalue" {
		t.Fatalf("extract fallback = %q", got)
	}
}
