// Package dns manages the per-cluster DNS rule that resolves the LeptoStack
// domain to the cluster gateway address. On Linux the rule is a
// NetworkManager/dnsmasq drop-in; on Windows, which has no wildcard hosts
// support, the CLI is guided-only and never modifies the OS.
//
// The Manager struct and Rule()/GuidedInstructions() renderers here are
// platform-neutral; EnsurePlugin/Update/Get/Delete are defined per OS in
// manager_*.go.
package dns

import (
	"fmt"
	"strings"

	"github.com/micronisus/leptostack-cli/internal/execx"
)

// Manager applies DNS changes.
type Manager struct {
	R      *execx.Runner
	Distro string
	// DropInDir allows overriding the drop-in directory in tests (Linux).
	DropInDir string
}

// Rule returns the dnsmasq rule line for a domain and target.
func Rule(domain, targetIP string) string {
	return fmt.Sprintf("address=/%s/%s", domain, targetIP)
}

// hostnamePrefixes are the documented service hostname prefixes from the
// cluster template's leptostack-config.yaml.
var hostnamePrefixes = []string{"sso", "che", "dashboard", "db", "design", "git", "llm", "registry"}

// GuidedInstructions renders the manual DNS setup steps for platforms without
// dnsmasq integration (Windows). It is pure and never modifies the OS, so the
// caller can print it (or a test can assert on it) safely.
func GuidedInstructions(domain, targetIP string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Windows has no wildcard DNS support, so *.%s cannot be resolved\n", domain)
	fmt.Fprintf(&b, "automatically. Add the concrete hostnames to the hosts file instead.\n\n")
	fmt.Fprintf(&b, "1. As Administrator, open:\n     %%SystemRoot%%\\System32\\drivers\\etc\\hosts\n")
	fmt.Fprintf(&b, "2. Add these lines (gateway IP %s):\n\n", targetIP)
	fmt.Fprintf(&b, "     %s %s\n", targetIP, domain)
	for _, p := range hostnamePrefixes {
		fmt.Fprintf(&b, "     %s %s.%s\n", targetIP, p, domain)
	}
	fmt.Fprintf(&b, "\n   If a service hostname was overridden with a leptostack-name\n")
	fmt.Fprintf(&b, "   prefix, add that form too (for example portal-sso.%s).\n\n", domain)
	fmt.Fprintf(&b, "3. Flush the resolver cache:\n     ipconfig /flushdns\n\n")
	fmt.Fprintf(&b, "Docs: https://github.com/micronisus/leptostack-cli#configuration\n")
	return b.String()
}
