//go:build darwin

package ca

import (
	"context"
	"fmt"

	"github.com/micronisus/leptostack-cli/internal/execx"
)

// install reports that CA trust integration is unsupported on macOS.
func (s Store) install(ctx context.Context, r *execx.Runner, distro, domain string) error {
	return fmt.Errorf("unsupported platform: CA trust integration is available on Fedora, Arch Linux and Windows")
}

// trustStatus cannot determine trust on macOS.
func (s Store) trustStatus(ctx context.Context, r *execx.Runner, distro, domain string) string {
	return "unknown"
}
