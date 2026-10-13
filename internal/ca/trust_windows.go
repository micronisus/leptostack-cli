//go:build windows

package ca

import (
	"context"
	"errors"
	"fmt"

	"github.com/micronisus/leptostack-cli/internal/execx"
)

// install adds the CA to the current user's Root certificate store via
// certutil. It targets the -user store, so no elevation (UAC) is required.
func (s Store) install(ctx context.Context, r *execx.Runner, distro, domain string) error {
	if _, ok := execx.LookPath("certutil"); !ok {
		return fmt.Errorf("certutil was not found on PATH; install the certificate manually with:\n  certutil -user -addstore -f Root %s", s.CertFile)
	}
	return r.Run(ctx, execx.Cmd{Name: "certutil", Args: []string{"-user", "-addstore", "-f", "Root", s.CertFile}})
}

// trustStatus probes the current user's Root store for the CA thumbprint.
// certutil output is locale-dependent, so the exit code of a thumbprint lookup
// is authoritative: a clean non-zero exit means "absent"; a tool or parse
// failure that never ran the lookup means "unknown".
func (s Store) trustStatus(ctx context.Context, r *execx.Runner, distro, domain string) string {
	if _, ok := execx.LookPath("certutil"); !ok {
		return "unknown"
	}
	thumb, err := certThumbprint(s.CertFile)
	if err != nil {
		return "unknown"
	}
	_, err = r.OutputCombined(ctx, execx.Cmd{Name: "certutil", Args: []string{"-user", "-verifystore", "Root", thumb}})
	if err == nil {
		return "installed"
	}
	var ee *execx.Error
	if errors.As(err, &ee) && ee.ExitCode >= 0 {
		return "absent"
	}
	return "unknown"
}
