package providers

import (
	"context"
	"fmt"
	"strconv"

	"github.com/micronisus/leptostack-cli/internal/execx"
)

// Minikube implements Provider for the minikube provider.
type Minikube struct {
	Deps
}

// Start starts the minikube profile and enables the required addons.
func (m *Minikube) Start(ctx context.Context) error {
	r := m.Resolved
	m.IO.Errf("Starting minikube (profile %s)...\n", r.MinikubeProfile)
	if err := m.R.Run(ctx, execx.Cmd{Name: "minikube", Args: []string{
		"start", "-p", r.MinikubeProfile, "--cni=calico",
		"--insecure-registry=registry." + r.Domain,
		"--cpus", strconv.Itoa(r.Minikube.CPUs),
		"--memory", strconv.Itoa(r.Minikube.MemoryMB),
	}}); err != nil {
		return err
	}
	m.IO.Errln()

	m.IO.Errln("Enabling minikube addons...")
	enabled, _ := m.R.OutputCombined(ctx, execx.Cmd{Name: "minikube", Args: []string{"-p", r.MinikubeProfile, "addons", "list", "--output=json"}})
	for _, addon := range []string{"metallb", "registry", "metrics-server"} {
		if addonEnabled(enabled, r.MinikubeProfile, addon) {
			m.IO.Errf("  %s already enabled, skipping.\n", addon)
			continue
		}
		if err := m.R.Run(ctx, execx.Cmd{Name: "minikube", Args: []string{"-p", r.MinikubeProfile, "addons", "enable", addon}}); err != nil {
			return err
		}
	}
	m.IO.Errln()
	return nil
}

// Stop stops the minikube profile.
func (m *Minikube) Stop(ctx context.Context) error {
	m.IO.Errln("Stopping minikube...")
	if err := m.R.Run(ctx, execx.Cmd{Name: "minikube", Args: []string{"stop", "-p", m.Resolved.MinikubeProfile}}); err != nil {
		return err
	}
	m.IO.Errln("Minikube stopped.")
	return nil
}

// Delete deletes the minikube profile.
func (m *Minikube) Delete(ctx context.Context) error {
	m.IO.Errln("Deleting minikube cluster...")
	return m.R.Run(ctx, execx.Cmd{Name: "minikube", Args: []string{"delete", "-p", m.Resolved.MinikubeProfile}})
}

// Status prints minikube status.
func (m *Minikube) Status(ctx context.Context) error {
	return m.R.Run(ctx, execx.Cmd{Name: "minikube", Args: []string{"status", "-p", m.Resolved.MinikubeProfile}})
}

// Running reports whether the minikube profile is running.
func (m *Minikube) Running(ctx context.Context) bool {
	return m.R.RunDiscard(ctx, execx.Cmd{Name: "minikube", Args: []string{"status", "-p", m.Resolved.MinikubeProfile}}) == nil
}

// IP returns the minikube IP.
func (m *Minikube) IP(ctx context.Context) string {
	out, err := m.R.Output(ctx, execx.Cmd{Name: "minikube", Args: []string{"ip", "-p", m.Resolved.MinikubeProfile}})
	if err != nil {
		return ""
	}
	return trim(out)
}

// EnsureContext verifies the derived kubectl context is selected.
func (m *Minikube) EnsureContext(ctx context.Context) error {
	cur := currentContext(ctx, m.R, m.Kubeconfig)
	if cur != m.Resolved.KubeContext {
		return fmt.Errorf("kubectl context is not set to %s (current: %q); run 'leptostack start'", m.Resolved.KubeContext, cur)
	}
	return nil
}

// addonEnabled reports whether an addon shows as enabled in `addons list -o json`.
func addonEnabled(jsonOut, profile, addon string) bool {
	needle := fmt.Sprintf("\"%s\":{\"Profile\":\"%s\",\"Status\":\"enabled\"", addon, profile)
	return contains(jsonOut, needle)
}
