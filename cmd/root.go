// Package cmd defines the cobra command tree for the leptostack CLI.
package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/micronisus/leptostack-cli/internal/cli"
	"github.com/micronisus/leptostack-cli/internal/upgrade"
	"github.com/micronisus/leptostack-cli/internal/version"
)

// Execute builds and runs the root command, returning the process exit code.
func Execute() int {
	// Remove a leftover binary from a previous Windows self-upgrade.
	upgrade.CleanupOld()
	build := version.Get()
	streams := cli.NewIOStreams(os.Stdin, os.Stdout, os.Stderr)
	flags := &cli.GlobalFlags{}
	app := cli.New(streams, flags, build)

	root := NewRootCmd(app)
	if err := root.Execute(); err != nil {
		fmt.Fprintf(streams.ErrOut, "Error: %v\n", err)
		return exitCodeFor(err)
	}
	return cli.ExitOK
}

// exitCodeFor maps an error to the documented exit-code contract, treating
// cobra usage errors (bad flags/args) as exit code 2.
func exitCodeFor(err error) int {
	code := cli.CodeOf(err)
	if code == cli.ExitFailure && isUsageError(err) {
		return cli.ExitUsage
	}
	return code
}

func isUsageError(err error) bool {
	s := err.Error()
	for _, marker := range []string{
		"arg(s)", "unknown command", "unknown flag", "unknown shorthand",
		"requires at least", "requires exactly", "invalid argument",
	} {
		if strings.Contains(s, marker) {
			return true
		}
	}
	return false
}

// NewRootCmd assembles the full command tree.
func NewRootCmd(app *cli.App) *cobra.Command {
	root := &cobra.Command{
		Use:   "leptostack",
		Short: "Manage the LeptoStack development environment",
		Long: `leptostack manages the LeptoStack local development environment:
provisioning a minikube or vcluster cluster, bootstrapping FluxCD GitOps,
managing Flux sources and secrets, pausing/restoring kustomizations, and
configuring local DNS and TLS trust.

Command grammar follows kubectl: 'leptostack <verb> <resource> [name]' for
object operations, and bare lifecycle verbs (start, stop, status) for the
selected cluster. Select a cluster with -c/--cluster or 'config use-context'.

Exit codes:
  0  success
  1  generic runtime failure
  2  usage error (bad flags/args)
  3  configuration missing or invalid
  4  prerequisite tool missing or too old
  5  timeout while waiting for a condition
  6  cluster not running or not reachable`,
		SilenceErrors: true,
		SilenceUsage:  true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}

	app.Flags.AddGlobalFlags(root.PersistentFlags())
	root.CompletionOptions.DisableDefaultCmd = true
	root.SetFlagErrorFunc(func(cmd *cobra.Command, err error) error {
		return cli.Exitf(cli.ExitUsage, "%v", err)
	})

	root.AddCommand(
		newVersionCmd(app),
		newUpgradeCmd(app),
		newCompletionCmd(app),
		newDoctorCmd(app),
		newConfigCmd(app),
		newStartCmd(app),
		newStopCmd(app),
		newRestartCmd(app),
		newStatusCmd(app),
		newConnectCmd(app),
		newDeleteCmd(app),
		newGetCmd(app),
		newCreateCmd(app),
		newReconcileCmd(app),
		newWaitCmd(app),
		newBootstrapCmd(app),
		newSyncCmd(app),
		newEnableCmd(app),
		newDisableCmd(app),
		newDNSCmd(app),
		newCACmd(app),
		newRegistryCmd(app),
		newPortForwardCmd(app),
		newLogsCmd(app),
	)

	return root
}
