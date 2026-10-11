package cli

import (
	"time"

	"github.com/spf13/pflag"
)

// GlobalFlags holds the conventional flags available on every command.
type GlobalFlags struct {
	Cluster        string
	Context        string
	Namespace      string
	Kubeconfig     string
	Output         string
	Yes            bool
	DryRun         bool
	Timeout        time.Duration
	Verbose        int
	NoColor        bool
	NonInteractive bool
}

// AddGlobalFlags registers the global flags on a flag set.
func (g *GlobalFlags) AddGlobalFlags(fs *pflag.FlagSet) {
	fs.StringVarP(&g.Cluster, "cluster", "c", "", "Select the configured cluster (default: current-context)")
	fs.StringVar(&g.Context, "context", "", "Override the kubectl context (default: derived per provider)")
	fs.StringVarP(&g.Namespace, "namespace", "n", "", "Namespace scope where applicable")
	fs.StringVar(&g.Kubeconfig, "kubeconfig", "", "kubeconfig to use (default: standard resolution)")
	fs.StringVarP(&g.Output, "output", "o", "", "Output format: wide, json, yaml, name, or human")
	fs.BoolVarP(&g.Yes, "yes", "y", false, "Skip confirmation prompts")
	fs.BoolVar(&g.DryRun, "dry-run", false, "Client-side validation only; no side effects")
	fs.DurationVar(&g.Timeout, "timeout", 0, "Command-level timeout (defaults per command)")
	fs.IntVarP(&g.Verbose, "verbose", "v", 0, "Log level 0-4")
	fs.BoolVar(&g.NoColor, "no-color", false, "Disable ANSI color (auto when output is not a TTY)")
	fs.BoolVar(&g.NonInteractive, "non-interactive", false, "Fail instead of prompting when a required value is missing")
}
