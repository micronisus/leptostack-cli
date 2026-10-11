package gitops

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

func pathExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

func fileExists(p string) bool {
	info, err := os.Stat(p)
	return err == nil && !info.IsDir()
}

func dirExists(p string) bool {
	info, err := os.Stat(p)
	return err == nil && info.IsDir()
}

// replaceContents removes everything in dest except .git, then copies src.
func replaceContents(src, dest string) error {
	entries, err := os.ReadDir(dest)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if e.Name() == ".git" {
			continue
		}
		if err := os.RemoveAll(filepath.Join(dest, e.Name())); err != nil {
			return err
		}
	}
	return copyContents(src, dest)
}

func copyContents(src, dest string) error {
	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if e.Name() == ".git" {
			continue
		}
		if err := copyPath(filepath.Join(src, e.Name()), filepath.Join(dest, e.Name())); err != nil {
			return err
		}
	}
	return nil
}

// copyPath copies a file or directory recursively, preserving modes.
func copyPath(src, dst string) error {
	info, err := os.Stat(src)
	if err != nil {
		return err
	}
	if info.IsDir() {
		if err := os.MkdirAll(dst, info.Mode().Perm()); err != nil {
			return err
		}
		entries, err := os.ReadDir(src)
		if err != nil {
			return err
		}
		for _, e := range entries {
			if err := copyPath(filepath.Join(src, e.Name()), filepath.Join(dst, e.Name())); err != nil {
				return err
			}
		}
		return nil
	}
	b, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	return os.WriteFile(dst, b, info.Mode().Perm())
}

// dirsEqual reports whether two paths have identical file trees.
func dirsEqual(a, b string) (bool, error) {
	ai, err := os.Stat(a)
	if err != nil {
		return false, nil
	}
	bi, err := os.Stat(b)
	if err != nil {
		return false, nil
	}
	if ai.IsDir() != bi.IsDir() {
		return false, nil
	}
	if !ai.IsDir() {
		ab, err1 := os.ReadFile(a)
		bb, err2 := os.ReadFile(b)
		if err1 != nil || err2 != nil {
			return false, nil
		}
		return bytes.Equal(ab, bb), nil
	}
	af, err := listFiles(a)
	if err != nil {
		return false, err
	}
	bf, err := listFiles(b)
	if err != nil {
		return false, err
	}
	if len(af) != len(bf) {
		return false, nil
	}
	for i := range af {
		if af[i] != bf[i] {
			return false, nil
		}
	}
	return true, nil
}

func listFiles(root string) ([]string, error) {
	var out []string
	err := filepath.Walk(root, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, p)
		out = append(out, fmt.Sprintf("%s|%d|%d", rel, info.Mode(), info.Size()))
		return nil
	})
	sort.Strings(out)
	return out, err
}
