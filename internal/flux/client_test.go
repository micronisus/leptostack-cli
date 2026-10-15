package flux

import (
	"context"
	"io"
	"strings"
	"testing"

	"github.com/micronisus/leptostack-cli/internal/cli"
	"github.com/micronisus/leptostack-cli/internal/execx"
	"github.com/micronisus/leptostack-cli/internal/testutil"
)

func fluxClient(t *testing.T) (*Client, *testutil.FakeEnv) {
	t.Helper()
	env := testutil.NewFakeEnv(t)
	env.Tool(t, "flux", "echo \"$*\" >> \"$FAKE_LOG\"\nexit 0\n")
	env.Active(t)
	r := execx.New(cli.NewIOStreams(strings.NewReader(""), io.Discard, io.Discard), 0)
	return New(r, "leptostack-local", ""), env
}

func TestBootstrapPassesCAFile(t *testing.T) {
	c, env := fluxClient(t)
	if err := c.Bootstrap(context.Background(), "gitea", "tok", "git.example.com", "team", "gitops", "main", "clusters/local", "/etc/ssl/enterprise-ca.crt"); err != nil {
		t.Fatal(err)
	}
	if !env.HasInvocation(t, "--ca-file=/etc/ssl/enterprise-ca.crt") {
		t.Fatalf("expected --ca-file in the flux bootstrap args; got %v", env.Invocations(t))
	}
	if !env.HasInvocation(t, "--hostname=git.example.com") {
		t.Fatalf("expected --hostname; got %v", env.Invocations(t))
	}
}

func TestBootstrapOmitsCAFileWhenUnset(t *testing.T) {
	c, env := fluxClient(t)
	if err := c.Bootstrap(context.Background(), "github", "tok", "", "team", "gitops", "main", "clusters/local", ""); err != nil {
		t.Fatal(err)
	}
	for _, line := range env.Invocations(t) {
		if strings.Contains(line, "--ca-file") {
			t.Fatalf("--ca-file must be omitted when unset; got %v", env.Invocations(t))
		}
	}
}
