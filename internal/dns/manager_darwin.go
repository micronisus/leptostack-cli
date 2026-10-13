//go:build darwin

package dns

import (
	"context"
	"fmt"
)

// EnsurePlugin reports that local DNS integration is unsupported on macOS.
func (m *Manager) EnsurePlugin(ctx context.Context) error {
	return fmt.Errorf("local DNS integration is unsupported on macOS; add the cluster hostnames to /etc/hosts manually")
}

// Update reports that local DNS integration is unsupported on macOS.
func (m *Manager) Update(ctx context.Context, cluster, domain, targetIP string) error {
	return fmt.Errorf("local DNS integration is unsupported on macOS; add %s hostnames to /etc/hosts manually", domain)
}

// Get reports that no managed DNS rule exists on macOS.
func (m *Manager) Get(cluster string) (string, bool) { return "", false }

// Delete is a no-op on macOS: there is no managed rule to remove.
func (m *Manager) Delete(ctx context.Context, cluster string) error { return nil }
