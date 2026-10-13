package cmd

import (
	"github.com/spf13/cobra"

	"github.com/micronisus/leptostack-cli/internal/cli"
)

func newReconcileCmd(app *cli.App) *cobra.Command {
	c := &cobra.Command{
		Use:     "reconcile",
		Aliases: []string{"reconcile-kustomization"},
		Short:   "Trigger Flux reconciliation",
		Args:    cobra.NoArgs,
		RunE:    func(cmd *cobra.Command, args []string) error { return cmd.Help() },
	}
	c.AddCommand(newReconcileKustomizationCmd(app), newReconcileSourceCmd(app))
	return c
}

func newReconcileKustomizationCmd(app *cli.App) *cobra.Command {
	var withSource bool
	c := &cobra.Command{
		Use:   "kustomization [name]",
		Short: "Reconcile a kustomization (default: flux-system)",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := "flux-system"
			if len(args) == 1 {
				name = args[0]
			}
			r, err := resolve(app)
			if err != nil {
				return err
			}
			ctx, cancel := app.Context(defaultReconcileTime)
			defer cancel()
			if err := requireReachable(ctx, app, kubeFor(app, r)); err != nil {
				return err
			}
			app.IO.Errf("Reconciling kustomization %s...\n", name)
			return fluxFor(app, r).ReconcileKustomization(ctx, name, withSource, app.Timeout(defaultReconcileTime))
		},
	}
	c.Flags().BoolVar(&withSource, "with-source", false, "Also reconcile the referenced source")
	return c
}

func newReconcileSourceCmd(app *cli.App) *cobra.Command {
	var sourceType string
	c := &cobra.Command{
		Use:   "source [name]",
		Short: "Reconcile a Flux source (default: git leptostack-base)",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := "leptostack-base"
			if len(args) == 1 {
				name = args[0]
			}
			r, err := resolve(app)
			if err != nil {
				return err
			}
			ctx, cancel := app.Context(defaultReconcileTime)
			defer cancel()
			if err := requireReachable(ctx, app, kubeFor(app, r)); err != nil {
				return err
			}
			app.IO.Errf("Reconciling %s source %s...\n", sourceType, name)
			return fluxFor(app, r).ReconcileSource(ctx, sourceType, name, app.Timeout(defaultReconcileTime))
		},
	}
	c.Flags().StringVar(&sourceType, "type", "git", "Source type: git, helm, bucket or oci")
	return c
}
