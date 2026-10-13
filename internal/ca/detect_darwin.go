//go:build darwin

package ca

import "fmt"

// DetectDistro reports that CA trust integration is unsupported on macOS. The
// binary still builds and runs; only `ca install`/`ca status` are unavailable.
func DetectDistro() (string, error) {
	return "", fmt.Errorf("unsupported platform: CA trust integration is available on Fedora, Arch Linux and Windows")
}
