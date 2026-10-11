// Package execx runs external tools (kubectl, flux, minikube, vcluster, git,
// NetworkManager) with context timeouts and structured errors. The Go CLI is
// an orchestration layer: it never reimplements the platform tools.
package execx

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"regexp"
	"strings"

	"github.com/micronisus/leptostack-cli/internal/cli"
)

// Cmd describes one external command invocation.
type Cmd struct {
	Name   string
	Args   []string
	Env    map[string]string
	Dir    string
	Stdin  io.Reader
	Stdout io.Writer
	Stderr io.Writer
	// ParseJSON, when true, is informational for callers; execution is the same.
	ParseJSON bool
}

// Error wraps a failed external command with its argv, exit code and output.
type Error struct {
	Name     string
	Args     []string
	ExitCode int
	Output   string
	Err      error
}

func (e *Error) Error() string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s %s failed", e.Name, redactArgs(e.Args))
	if e.ExitCode >= 0 {
		fmt.Fprintf(&b, " (exit %d)", e.ExitCode)
	}
	if tail := tailLines(redactCredentials(e.Output), 5); tail != "" {
		fmt.Fprintf(&b, ":\n%s", tail)
	} else if e.Err != nil {
		fmt.Fprintf(&b, ": %v", e.Err)
	}
	return b.String()
}

func (e *Error) Unwrap() error { return e.Err }

// IsNotFound reports whether an error indicates a missing object (exit 1 with
// a NotFound/not found payload), which callers treat as "absent".
func (e *Error) IsNotFound() bool {
	o := strings.ToLower(e.Output)
	return strings.Contains(o, "not found") || strings.Contains(o, "notfound") || strings.Contains(o, "no such")
}

// Runner executes commands, optionally streaming through IOStreams.
type Runner struct {
	IO      *cli.IOStreams
	Verbose int
}

// New builds a Runner.
func New(io *cli.IOStreams, verbose int) *Runner {
	return &Runner{IO: io, Verbose: verbose}
}

func (r *Runner) env(cmd *Cmd) []string {
	if len(cmd.Env) == 0 {
		return nil
	}
	env := os.Environ()
	for k, v := range cmd.Env {
		env = append(env, k+"="+v)
	}
	return env
}

func (r *Runner) trace(cmd *Cmd) {
	if r.Verbose >= 2 && r.IO != nil {
		r.IO.Errf("+ %s %s\n", cmd.Name, redactArgs(cmd.Args))
	}
}

// credentialURLRe matches the userinfo component of a URL (scheme://user:pass@).
var credentialURLRe = regexp.MustCompile(`([a-zA-Z][a-zA-Z0-9+.-]*://)[^/@\s]*@`)

// redactCredentials strips URL userinfo (which may carry tokens) from text
// before it is logged or returned in an error.
func redactCredentials(s string) string {
	if !strings.Contains(s, "://") {
		return s
	}
	return credentialURLRe.ReplaceAllString(s, "${1}***@")
}

func redactArgs(args []string) string {
	out := make([]string, len(args))
	for i, a := range args {
		out[i] = redactCredentials(a)
	}
	return strings.Join(out, " ")
}

// Output runs the command and returns its stdout as a trimmed string.
func (r *Runner) Output(ctx context.Context, cmd Cmd) (string, error) {
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := r.exec(ctx, &cmd)
	if err != nil {
		return "", r.wrap(cmd, stdout.String()+stderr.String(), err)
	}
	return strings.TrimRight(stdout.String(), "\n"), nil
}

// OutputBytes runs the command and returns raw stdout bytes.
func (r *Runner) OutputBytes(ctx context.Context, cmd Cmd) ([]byte, error) {
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := r.exec(ctx, &cmd)
	if err != nil {
		return nil, r.wrap(cmd, stdout.String()+stderr.String(), err)
	}
	return stdout.Bytes(), nil
}

// OutputCombined runs the command and returns stdout+stderr combined, used when
// the interesting payload may be on stderr (e.g. kubectl jsonpath warnings).
func (r *Runner) OutputCombined(ctx context.Context, cmd Cmd) (string, error) {
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	err := r.exec(ctx, &cmd)
	if err != nil {
		return strings.TrimRight(buf.String(), "\n"), r.wrap(cmd, buf.String(), err)
	}
	return strings.TrimRight(buf.String(), "\n"), nil
}

// Run executes the command, streaming stdout/stderr to the caller's streams
// unless explicit writers are set. It returns the combined output on failure.
func (r *Runner) Run(ctx context.Context, cmd Cmd) error {
	var buf bytes.Buffer
	if cmd.Stdout == nil {
		if r.IO != nil {
			cmd.Stdout = io.MultiWriter(r.IO.Out, &buf)
		} else {
			cmd.Stdout = &buf
		}
	}
	if cmd.Stderr == nil {
		if r.IO != nil {
			cmd.Stderr = io.MultiWriter(r.IO.ErrOut, &buf)
		} else {
			cmd.Stderr = &buf
		}
	}
	if cmd.Stdin == nil && r.IO != nil {
		cmd.Stdin = r.IO.In
	}
	err := r.exec(ctx, &cmd)
	if err != nil {
		return r.wrap(cmd, buf.String(), err)
	}
	return nil
}

// RunDiscard executes the command discarding all output.
func (r *Runner) RunDiscard(ctx context.Context, cmd Cmd) error {
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	err := r.exec(ctx, &cmd)
	if err != nil {
		return r.wrap(cmd, "", err)
	}
	return nil
}

func (r *Runner) exec(ctx context.Context, cmd *Cmd) error {
	r.trace(cmd)
	c := exec.CommandContext(ctx, cmd.Name, cmd.Args...)
	c.Env = r.env(cmd)
	c.Dir = cmd.Dir
	c.Stdin = cmd.Stdin
	c.Stdout = cmd.Stdout
	c.Stderr = cmd.Stderr
	return c.Run()
}

func (r *Runner) wrap(cmd Cmd, output string, err error) error {
	code := -1
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		code = ee.ExitCode()
	}
	return &Error{Name: cmd.Name, Args: cmd.Args, ExitCode: code, Output: strings.TrimSpace(output), Err: err}
}

// LookPath reports whether an executable is available on PATH.
func LookPath(name string) (string, bool) {
	p, err := exec.LookPath(name)
	if err != nil {
		return "", false
	}
	return p, true
}

func tailLines(s string, n int) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	lines := strings.Split(s, "\n")
	if len(lines) <= n {
		return s
	}
	return strings.Join(lines[len(lines)-n:], "\n")
}
