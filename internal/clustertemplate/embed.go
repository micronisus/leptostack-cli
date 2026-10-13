// Package clustertemplate embeds the LeptoStack cluster template. The template
// was historically cloned from an external git repository at runtime; it is now
// a one-time snapshot vendored into the CLI and embedded in the binary.
package clustertemplate

import (
	"embed"
	"io/fs"
	"os"
	"path/filepath"
)

//go:embed all:template
var templateFS embed.FS

// Extract writes the embedded cluster template into dst. Files are written with
// mode 0644 (directories 0755) rather than the read-only mode reported by
// embedded files, because generation edits them in place afterwards.
func Extract(dst string) error {
	return fs.WalkDir(templateFS, "template", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel("template", p)
		if err != nil {
			return err
		}
		if rel == "." {
			return os.MkdirAll(dst, 0o755)
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		b, err := templateFS.ReadFile(p)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		return os.WriteFile(target, b, 0o644)
	})
}
