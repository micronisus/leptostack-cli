package clustertemplate

import (
	"os"
	"path/filepath"
	"testing"
)

func TestExtract(t *testing.T) {
	dir := t.TempDir()
	if err := Extract(dir); err != nil {
		t.Fatal(err)
	}

	files := []string{
		"README.md",
		"clusters/example/kustomization.yaml",
		"infrastructure/kustomization.yaml",
		"infrastructure/leptostack-source.yaml",
		"apps/leptostack/base/kustomization.yaml",
		"apps/leptostack/overlays/example/kustomization.yaml",
	}
	for _, rel := range files {
		p := filepath.Join(dir, filepath.FromSlash(rel))
		if _, err := os.Stat(p); err != nil {
			t.Errorf("%s: %v", rel, err)
		}
	}

	// Zero-byte files must be preserved (flux bootstrap fills them later).
	empty := filepath.Join(dir, "clusters", "example", "flux-system", "gotk-components.yaml")
	info, err := os.Stat(empty)
	if err != nil {
		t.Fatalf("empty template file missing: %v", err)
	}
	if info.Size() != 0 {
		t.Errorf("gotk-components.yaml should stay empty, size = %d", info.Size())
	}

	// Extracted files must be writable: generation edits them in place.
	writeProbe := filepath.Join(dir, "infrastructure", "leptostack-source.yaml")
	if err := os.WriteFile(writeProbe, []byte("x"), 0o644); err != nil {
		t.Fatalf("extracted file must be writable: %v", err)
	}
}
