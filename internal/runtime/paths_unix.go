//go:build !windows

package runtime

import (
	"fmt"
	"os"
	"path/filepath"
)

// BaseDir returns the per-user runtime root for leptostack.
func BaseDir() string {
	if dir := os.Getenv("XDG_RUNTIME_DIR"); dir != "" {
		return filepath.Join(dir, "leptostack")
	}
	return filepath.Join(os.TempDir(), fmt.Sprintf("leptostack-%d", os.Getuid()))
}

// LegacyPortForwardDir returns the bash-era shared state directory, used to
// interoperate with background forwards started by the old script.
func LegacyPortForwardDir() string {
	if dir := os.Getenv("XDG_RUNTIME_DIR"); dir != "" {
		return filepath.Join(dir, "leptostack", "port-forwards")
	}
	return filepath.Join(os.TempDir(), fmt.Sprintf("leptostack-port-forwards-%d", os.Getuid()))
}
