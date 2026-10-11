// Package testutil provides a fake-binary harness: shim scripts for kubectl,
// flux, minikube, vcluster and git that record their argv and emit canned
// responses, so command orchestration can be exercised without a real cluster.
package testutil

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// FakeEnv is a temporary bin directory plus an invocation log.
type FakeEnv struct {
	BinDir  string
	LogPath string
}

// NewFakeEnv creates a fake environment under t.TempDir().
func NewFakeEnv(t *testing.T) *FakeEnv {
	t.Helper()
	dir := t.TempDir()
	bin := filepath.Join(dir, "bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatalf("mkdir bin: %v", err)
	}
	return &FakeEnv{BinDir: bin, LogPath: filepath.Join(dir, "invocations.log")}
}

// Tool writes an executable shim for name. The body is a POSIX shell script and
// may use $FAKE_LOG to append to the invocation log.
func (f *FakeEnv) Tool(t *testing.T, name, body string) {
	t.Helper()
	script := "#!/bin/sh\n" + body + "\n"
	path := filepath.Join(f.BinDir, name)
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("write shim %s: %v", name, err)
	}
}

// Active configures PATH (fake bin first) and FAKE_LOG on the test process.
func (f *FakeEnv) Active(t *testing.T) {
	t.Helper()
	t.Setenv("PATH", f.BinDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("FAKE_LOG", f.LogPath)
}

// Invocations returns the recorded command lines.
func (f *FakeEnv) Invocations(t *testing.T) []string {
	t.Helper()
	b, err := os.ReadFile(f.LogPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		t.Fatalf("read log: %v", err)
	}
	var out []string
	for _, line := range strings.Split(strings.TrimRight(string(b), "\n"), "\n") {
		if line != "" {
			out = append(out, line)
		}
	}
	return out
}

// HasInvocation reports whether any recorded line contains substr.
func (f *FakeEnv) HasInvocation(t *testing.T, substr string) bool {
	t.Helper()
	for _, line := range f.Invocations(t) {
		if strings.Contains(line, substr) {
			return true
		}
	}
	return false
}

// Kubeconfig writes a config file into a temp HOME and returns the config path.
func SetHomeConfig(t *testing.T, yaml string) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	cfgDir := filepath.Join(home, ".config", "leptostack")
	if err := os.MkdirAll(cfgDir, 0o700); err != nil {
		t.Fatalf("mkdir config: %v", err)
	}
	path := filepath.Join(cfgDir, "config.yaml")
	if yaml != "" {
		if err := os.WriteFile(path, []byte(yaml), 0o600); err != nil {
			t.Fatalf("write config: %v", err)
		}
	}
	t.Setenv("LEPTOSTACK_CONFIG", path)
	rt := filepath.Join(home, "run")
	if err := os.MkdirAll(rt, 0o700); err != nil {
		t.Fatalf("mkdir runtime: %v", err)
	}
	t.Setenv("XDG_RUNTIME_DIR", rt)
	return path
}
