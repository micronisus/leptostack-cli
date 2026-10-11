package cmd

import (
	"github.com/spf13/cobra"

	"github.com/micronisus/leptostack-cli/internal/cli"
	"github.com/micronisus/leptostack-cli/internal/kustomizations"
)

func newEnableCmd(app *cli.App) *cobra.Command {
	c := &cobra.Command{
		Use:     "enable",
		Aliases: []string{"resume"},
		Short:   "Resume reconciliation of kustomizations",
		Args:    cobra.NoArgs,
		RunE:    func(cmd *cobra.Command, args []string) error { return cmd.Help() },
	}
	c.AddCommand(newToggleKustomizationCmd(app, true))
	return c
}

func newDisableCmd(app *cli.App) *cobra.Command {
	c := &cobra.Command{
		Use:     "disable",
		Aliases: []string{"suspend"},
		Short:   "Pause reconciliation of kustomizations",
		Args:    cobra.NoArgs,
		RunE:    func(cmd *cobra.Command, args []string) error { return cmd.Help() },
	}
	c.AddCommand(newToggleKustomizationCmd(app, false))
	return c
}

func newToggleKustomizationCmd(app *cli.App, enable bool) *cobra.Command {
	var includeFluxSystem bool
	verb := "enable"
	if !enable {
		verb = "disable"
	}
	c := &cobra.Command{
		Use:   "kustomization <name>|all",
		Short: verb + " reconciliation of a kustomization (or all)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			r, err := resolve(app)
			if err != nil {
				return err
			}
			ctx, cancel := app.Context(clusterWaitTimeout)
			defer cancel()
			kube := kubeFor(app, r)
			if err := requireReachable(ctx, app, kube); err != nil {
				return err
			}

			names := []string{args[0]}
			if args[0] == "all" {
				names, err = kustomizations.AllNames(ctx, kube, includeFluxSystem)
				if err != nil {
					return err
				}
				if !includeFluxSystem {
					app.IO.Errln("Note: 'flux-system' is excluded from 'all'; pass --include-flux-system to include it.")
				}
			}

			var changed []string
			if enable {
				changed, err = kustomizations.Enable(ctx, kube, names)
			} else {
				changed, err = kustomizations.Disable(ctx, kube, names)
			}
			if err != nil {
				return err
			}
			for _, n := range changed {
				app.IO.Errf("Kustomization %s %sd.\n", n, verb)
			}
			if len(changed) == 0 {
				app.IO.Errln("No kustomizations matched.")
			}
			return nil
		},
	}
	c.Flags().BoolVar(&includeFluxSystem, "include-flux-system", false, "Include flux-system in 'all'")
	return c
}
