//go:build windows

package dns

import (
	"context"
	"io"
	"os"
)

// EnsurePlugin is a no-op on Windows: there is no NetworkManager/dnsmasq.
func (m *Manager) EnsurePlugin(ctx context.Context) error { return nil }

// Update prints guided instructions and never modifies the OS, so it needs no
// elevation and always succeeds.
func (m *Manager) Update(ctx context.Context, cluster, domain, targetIP string) error {
	m.print(GuidedInstructions(domain, targetIP))
	return nil
}

// Get reports that no OS-level rule is configured on Windows.
func (m *Manager) Get(cluster string) (string, bool) { return "", false }

// Delete prints cleanup guidance and never modifies the OS.
func (m *Manager) Delete(ctx context.Context, cluster string) error {
	m.print("Windows DNS is configured manually: remove the leptostack lines from\n" +
		"  %SystemRoot%\\System32\\drivers\\etc\\hosts\n" +
		"(as Administrator) and flush the resolver cache with:\n  ipconfig /flushdns\n")
	return nil
}

func (m *Manager) print(text string) {
	var w io.Writer = os.Stderr
	if m.R != nil && m.R.IO != nil {
		w = m.R.IO.ErrOut
	}
	_, _ = io.WriteString(w, text)
}
