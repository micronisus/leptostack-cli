package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/micronisus/leptostack-cli/internal/cli"
	"github.com/micronisus/leptostack-cli/internal/testutil"
)

func TestGenerateFluxCDNoConfig(t *testing.T) {
	// The command must work without any configuration.
	testutil.SetHomeConfig(t, "")
	dst := t.TempDir()
	_, errOut, code := runCLI(t, "", "generate", "fluxcd", dst)
	if code != 0 {
		t.Fatalf("exit code = %d (stderr=%s)", code, errOut)
	}
	for _, rel := range []string{
		"README.md",
		"clusters/example/kustomization.yaml",
		"infrastructure/kustomization.yaml",
		"apps/leptostack/base/kustomization.yaml",
		"apps/ai/base/kustomization.yaml",
		"apps/ai/overlays/example/kustomization.yaml",
	} {
		if _, err := os.Stat(filepath.Join(dst, filepath.FromSlash(rel))); err != nil {
			t.Errorf("%s: %v", rel, err)
		}
	}
}

func TestGenerateFluxCDNonEmptyRequiresForce(t *testing.T) {
	testutil.SetHomeConfig(t, "")
	dst := t.TempDir()
	if err := os.WriteFile(filepath.Join(dst, "keep.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, code := runCLI(t, "", "generate", "fluxcd", dst); code != cli.ExitUsage {
		t.Fatalf("exit code = %d want %d", code, cli.ExitUsage)
	}
	if _, errOut, code := runCLI(t, "", "generate", "fluxcd", dst, "--force"); code != 0 {
		t.Fatalf("exit code = %d (stderr=%s)", code, errOut)
	}
	if _, err := os.Stat(filepath.Join(dst, "clusters", "example", "kustomization.yaml")); err != nil {
		t.Fatalf("template not written with --force: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dst, "keep.txt")); err != nil {
		t.Fatalf("--force must not remove existing files: %v", err)
	}
}

func TestGenerateFluxCDDryRun(t *testing.T) {
	testutil.SetHomeConfig(t, "")
	dst := t.TempDir()
	_, errOut, code := runCLI(t, "", "generate", "fluxcd", dst, "--dry-run")
	if code != 0 {
		t.Fatalf("exit code = %d", code)
	}
	if !strings.Contains(errOut, "Would export") {
		t.Fatalf("expected dry-run message, got %q", errOut)
	}
	entries, _ := os.ReadDir(dst)
	if len(entries) != 0 {
		t.Fatalf("dry-run must not write files: %v", entries)
	}
}
