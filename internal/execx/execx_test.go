package execx

import (
	"strings"
	"testing"
)

func TestErrorRedactsCredentials(t *testing.T) {
	e := &Error{
		Name:     "git",
		Args:     []string{"clone", "https://oauth2:s3cr3t@github.com/acme/repo.git", "/tmp/repo"},
		ExitCode: 128,
		Output:   "fatal: could not read from https://oauth2:s3cr3t@github.com/acme/repo.git",
	}
	msg := e.Error()
	if strings.Contains(msg, "s3cr3t") {
		t.Fatalf("credential leaked in error: %s", msg)
	}
	if !strings.Contains(msg, "***@github.com") {
		t.Fatalf("expected redacted userinfo, got: %s", msg)
	}
}

func TestRedactArgsLeavesPlainArgsAlone(t *testing.T) {
	args := []string{"get", "pods", "-n", "flux-system"}
	if got := redactArgs(args); got != strings.Join(args, " ") {
		t.Fatalf("plain args changed: %q", got)
	}
}
