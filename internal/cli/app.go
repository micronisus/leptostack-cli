package cli

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/micronisus/leptostack-cli/internal/config"
	"github.com/micronisus/leptostack-cli/internal/ui"
	"github.com/micronisus/leptostack-cli/internal/version"
)

// App carries the shared IO streams, global flags and lazily-resolved
// configuration through every command.
type App struct {
	IO        *IOStreams
	Flags     *GlobalFlags
	BuildInfo version.Info

	cfg       *config.Config
	resolved  *config.Resolved
	cfgLoaded bool
	cfgErr    error
	migrated  bool
}

// New builds an App.
func New(io *IOStreams, flags *GlobalFlags, build version.Info) *App {
	return &App{IO: io, Flags: flags, BuildInfo: build}
}

// Config loads (with legacy migration) and caches the configuration.
func (a *App) Config() (*config.Config, error) {
	if a.cfgLoaded {
		return a.cfg, a.cfgErr
	}
	a.cfgLoaded = true
	cfg, migrated, err := config.LoadWithMigration()
	if err != nil {
		a.cfgErr = MapConfigError(err)
		return nil, a.cfgErr
	}
	a.cfg = cfg
	a.migrated = migrated
	if migrated {
		a.IO.Errf("Imported legacy configuration into %s (original file kept).\n", config.DefaultPath())
	}
	return cfg, nil
}

// Migrated reports whether a legacy config was imported this run.
func (a *App) Migrated() bool { return a.migrated }

// Resolve returns the effective per-cluster configuration.
func (a *App) Resolve() (*config.Resolved, error) {
	if a.resolved != nil {
		return a.resolved, nil
	}
	cfg, err := a.Config()
	if err != nil {
		return nil, err
	}
	r, err := cfg.Resolve(a.Flags.Cluster)
	if err != nil {
		return nil, MapConfigError(err)
	}
	if a.Flags.Context != "" {
		r.KubeContext = a.Flags.Context
	}
	a.resolved = r
	return r, nil
}

// Invalidate drops cached config/resolution (used after config mutations).
func (a *App) Invalidate() {
	a.cfg = nil
	a.resolved = nil
	a.cfgLoaded = false
	a.cfgErr = nil
}

// Colorizer returns a colorizer honoring --no-color and TTY detection.
func (a *App) Colorizer() ui.Colorizer {
	enabled := !a.Flags.NoColor && a.IO.IsStdoutTTY()
	return ui.Colorizer{Enabled: enabled}
}

// Interactive reports whether prompts are allowed.
func (a *App) Interactive() bool {
	if a.Flags.NonInteractive {
		return false
	}
	return a.IO.IsStdinTTY()
}

// Confirm asks a yes/no question. --yes short-circuits to true; a
// non-interactive session without --yes fails with a usage error.
func (a *App) Confirm(prompt string) (bool, error) {
	if a.Flags.Yes {
		return true, nil
	}
	if !a.Interactive() {
		return false, Exitf(ExitUsage, "confirmation required but stdin is not a terminal; re-run with --yes")
	}
	a.IO.Printf("%s [y/N]: ", prompt)
	line, err := a.IO.ReadLine()
	if err != nil {
		return false, err
	}
	line = strings.TrimSpace(strings.ToLower(line))
	return line == "y" || line == "yes", nil
}

// Prompt reads a required value, applying a default. In non-interactive mode a
// missing value is an error.
func (a *App) Prompt(label, def string) (string, error) {
	if !a.Interactive() {
		if def != "" {
			return def, nil
		}
		return "", Exitf(ExitUsage, "missing required value %q; provide it via a flag or run interactively", label)
	}
	if def != "" {
		a.IO.Printf("%s [%s]: ", label, def)
	} else {
		a.IO.Printf("%s: ", label)
	}
	line, err := a.IO.ReadLine()
	if err != nil {
		if errors.Is(err, context.Canceled) {
			return "", err
		}
		return "", err
	}
	line = strings.TrimSpace(line)
	if line == "" {
		line = def
	}
	return line, nil
}

// Timeout returns the effective timeout for a command given its default.
func (a *App) Timeout(def time.Duration) time.Duration {
	if a.Flags.Timeout > 0 {
		return a.Flags.Timeout
	}
	return def
}

// Context derives a context.Context honoring --timeout.
func (a *App) Context(def time.Duration) (context.Context, context.CancelFunc) {
	t := a.Timeout(def)
	if t <= 0 {
		return context.WithCancel(context.Background())
	}
	return context.WithTimeout(context.Background(), t)
}

// Errf writes to stderr.
func (a *App) Errf(format string, args ...any) { a.IO.Errf(format, args...) }

// Errln writes a line to stderr.
func (a *App) Errln(args ...any) { a.IO.Errln(args...) }

// MapConfigError maps config errors to the documented exit-code contract.
func MapConfigError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, config.ErrNotConfigured) {
		return WithExitCode(ExitConfig, err)
	}
	var ve *config.ValidationError
	if errors.As(err, &ve) {
		return WithExitCode(ExitConfig, err)
	}
	return err
}

// NoteMigration prints a one-line notice when a legacy config was imported.
func (a *App) NoteMigration() {
	if a.migrated {
		a.IO.Errf("Imported legacy configuration into %s (original file kept).\n", config.DefaultPath())
	}
}
