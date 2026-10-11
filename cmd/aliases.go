package cmd

import (
	"strings"

	"github.com/spf13/cobra"

	"github.com/micronisus/leptostack-cli/internal/cli"
	"github.com/micronisus/leptostack-cli/internal/portforward"
)

// registerDeprecatedAliases wires the old flat command names to the new
// grammar. Each prints a one-line stderr hint before delegating.
func registerDeprecatedAliases(root *cobra.Command, app *cli.App) {
	addDeprecated(root, app, "configure", []string{"config", "init"})
	addDeprecated(root, app, "update-dns", []string{"dns", "update"})
	addDeprecated(root, app, "add-trust", []string{"ca", "install"})
	addDeprecated(root, app, "reconnect", []string{"connect"})
	addDeprecated(root, app, "get-context", []string{"get", "context"})
	addDeprecated(root, app, "set-registry-creds", []string{"registry", "set-credentials"})
	addDeprecated(root, app, "update-flux", []string{"sync", "flux"})
	addDeprecated(root, app, "rebootstrap", []string{"bootstrap"})
	addDeprecated(root, app, "reset", []string{"delete"})
	root.AddCommand(newDeprecatedEventsCmd(app))
}

func addDeprecated(root *cobra.Command, app *cli.App, name string, target []string) {
	hint := strings.Join(target, " ")
	c := &cobra.Command{
		Use:                name,
		Short:              "Deprecated: use 'leptostack " + hint + "'",
		Hidden:             true,
		Args:               cobra.ArbitraryArgs,
		DisableFlagParsing: false,
		RunE: func(cmd *cobra.Command, args []string) error {
			app.IO.Errf("%s is deprecated; use 'leptostack %s'.\n", name, hint)
			tc, _, err := root.Find(target)
			if err != nil {
				return err
			}
			if tc.RunE == nil {
				return tc.Help()
			}
			return tc.RunE(tc, args)
		},
	}
	root.AddCommand(c)
}

func newDeprecatedEventsCmd(app *cli.App) *cobra.Command {
	return &cobra.Command{
		Use:    "events",
		Short:  "Deprecated: use 'leptostack get events'",
		Hidden: true,
		Args:   cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			app.IO.Errf("events is deprecated; use 'leptostack get events -w'.\n")
			r, err := resolve(app)
			if err != nil {
				return err
			}
			ctx, cancel := app.Context(0)
			defer cancel()
			return kubeFor(app, r).Run(ctx, "events", "-A", "-w")
		},
	}
}

// stopAllPortForwards supports the legacy `port-forward stop` spelling.
func stopAllPortForwards(app *cli.App, clusterName string) int {
	states, err := portforward.ListStates(clusterName)
	if err != nil {
		return 0
	}
	stopped := 0
	for i := range states {
		if states[i].Alive() {
			_ = states[i].Kill()
			app.IO.Errf("Stopped port-forward for %s (pid %d).\n", states[i].Service, states[i].PID)
			stopped++
		}
		portforward.RemoveState(clusterName, states[i].Service)
	}
	stopped += portforward.LegacyStopForwards()
	return stopped
}
