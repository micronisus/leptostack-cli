//go:build linux

package dns

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/micronisus/leptostack-cli/internal/execx"
)

// Dir is the NetworkManager dnsmasq drop-in directory.
const Dir = "/etc/NetworkManager/dnsmasq.d"

// RulePath returns the rule file path for a cluster.
func RulePath(cluster string) string {
	return filepath.Join(Dir, "leptostack-"+cluster+".conf")
}

func (m *Manager) dropInDir() string {
	if m.DropInDir != "" {
		return m.DropInDir
	}
	return Dir
}

// PluginConfigured reports whether NetworkManager uses the dnsmasq plugin.
func (m *Manager) PluginConfigured() bool {
	if !fileContains("/etc/NetworkManager/NetworkManager.conf", "dns=dnsmasq") {
		entries, err := os.ReadDir("/etc/NetworkManager/conf.d")
		if err != nil {
			return false
		}
		for _, e := range entries {
			if fileContains(filepath.Join("/etc/NetworkManager/conf.d", e.Name()), "dns=dnsmasq") {
				return true
			}
		}
		return false
	}
	return true
}

// EnsurePlugin configures the dnsmasq plugin when absent (Fedora only).
func (m *Manager) EnsurePlugin(ctx context.Context) error {
	if m.PluginConfigured() {
		return nil
	}
	switch m.Distro {
	case "fedora":
		if _, err := m.R.Output(ctx, execx.Cmd{Name: "sudo", Args: []string{"systemctl", "disable", "--now", "systemd-resolved"}}); err != nil {
			return err
		}
		_ = m.R.RunDiscard(ctx, execx.Cmd{Name: "sudo", Args: []string{"rm", "-f", "/etc/resolv.conf"}})
		conf := "/etc/NetworkManager/NetworkManager.conf"
		if fileContains(conf, "[main]") {
			if err := m.R.Run(ctx, execx.Cmd{
				Name: "sudo",
				Args: []string{"sed", "-i", `/^\[main\]/a dns=dnsmasq`, conf},
			}); err != nil {
				return err
			}
		} else {
			if err := m.R.Run(ctx, execx.Cmd{
				Name:  "sudo",
				Args:  []string{"tee", "-a", conf},
				Stdin: strings.NewReader("[main]\ndns=dnsmasq\n"),
			}); err != nil {
				return err
			}
		}
		return m.R.RunDiscard(ctx, execx.Cmd{Name: "sudo", Args: []string{"systemctl", "restart", "NetworkManager"}})
	case "arch":
		return fmt.Errorf("please configure NetworkManager to use dnsmasq (see https://wiki.archlinux.org/title/NetworkManager#dnsmasq)")
	default:
		return fmt.Errorf("NetworkManager is not configured with the dnsmasq plugin")
	}
}

// Update writes the rule and restarts NetworkManager.
func (m *Manager) Update(ctx context.Context, cluster, domain, targetIP string) error {
	if err := os.MkdirAll(m.dropInDir(), 0o755); err != nil {
		// Fall back to sudo for the real system path.
		if err := m.R.RunDiscard(ctx, execx.Cmd{Name: "sudo", Args: []string{"mkdir", "-p", m.dropInDir()}}); err != nil {
			return err
		}
	}
	content := Rule(domain, targetIP) + "\n"
	path := filepath.Join(m.dropInDir(), "leptostack-"+cluster+".conf")
	cmd := execx.Cmd{Name: "sudo", Args: []string{"tee", path}, Stdin: strings.NewReader(content)}
	if m.DropInDir != "" {
		// Test mode: write directly.
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			return err
		}
	} else {
		if err := m.R.RunDiscard(ctx, cmd); err != nil {
			return err
		}
	}
	return m.R.RunDiscard(ctx, execx.Cmd{Name: "sudo", Args: []string{"systemctl", "restart", "NetworkManager"}})
}

// Get reads the effective rule for a cluster.
func (m *Manager) Get(cluster string) (string, bool) {
	b, err := os.ReadFile(filepath.Join(m.dropInDir(), "leptostack-"+cluster+".conf"))
	if err != nil {
		return "", false
	}
	return strings.TrimSpace(string(b)), true
}

// Delete removes the rule file and restarts NetworkManager.
func (m *Manager) Delete(ctx context.Context, cluster string) error {
	path := filepath.Join(m.dropInDir(), "leptostack-"+cluster+".conf")
	if err := m.R.RunDiscard(ctx, execx.Cmd{Name: "sudo", Args: []string{"rm", "-f", path}}); err != nil {
		return err
	}
	return m.R.RunDiscard(ctx, execx.Cmd{Name: "sudo", Args: []string{"systemctl", "restart", "NetworkManager"}})
}

func fileContains(path, needle string) bool {
	b, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	return strings.Contains(string(b), needle)
}
