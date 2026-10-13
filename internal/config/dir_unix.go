//go:build !windows

package config

import (
	"os"
	"path/filepath"
)

// Dir returns the configuration directory (~/.config/leptostack), honoring the
// LEPTOSTACK_CONFIG_DIR override so the CLI CA/config can live on a mounted
// persistent volume.
func Dir() string {
	if dir := os.Getenv(EnvConfigDir); dir != "" {
		return dir
	}
	return filepath.Join(HomeDir(), ".config", "leptostack")
}
