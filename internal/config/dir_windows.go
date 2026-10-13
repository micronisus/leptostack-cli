//go:build windows

package config

import (
	"os"
	"path/filepath"
)

// Dir returns the configuration directory (%APPDATA%\leptostack), honoring the
// LEPTOSTACK_CONFIG_DIR override so the CLI CA/config can live on a mounted
// persistent volume.
func Dir() string {
	if dir := os.Getenv(EnvConfigDir); dir != "" {
		return dir
	}
	if dir, err := os.UserConfigDir(); err == nil && dir != "" {
		return filepath.Join(dir, "leptostack")
	}
	if appData := os.Getenv("APPDATA"); appData != "" {
		return filepath.Join(appData, "leptostack")
	}
	return filepath.Join(HomeDir(), "leptostack")
}
