//go:build windows

package runtime

import (
	"os"
	"path/filepath"
)

// BaseDir returns the per-user runtime root for leptostack
// (%LOCALAPPDATA%\leptostack).
func BaseDir() string {
	if local := os.Getenv("LOCALAPPDATA"); local != "" {
		return filepath.Join(local, "leptostack")
	}
	if dir, err := os.UserCacheDir(); err == nil && dir != "" {
		return filepath.Join(dir, "leptostack")
	}
	return filepath.Join(os.TempDir(), "leptostack")
}

// LegacyPortForwardDir has no Windows equivalent: the bash CLI never ran on
// Windows, so there is no legacy state to interoperate with.
func LegacyPortForwardDir() string { return "" }
