package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/micronisus/leptostack-cli/internal/cli"
	"github.com/micronisus/leptostack-cli/internal/gitops"
)

func newSyncCmd(app *cli.App) *cobra.Command {
	c := &cobra.Command{
		Use:   "sync",
		Short: "Refresh GitOps manifests",
		Args:  cobra.NoArgs,
		RunE:  func(cmd *cobra.Command, args []string) error { return cmd.Help() },
	}
	c.AddCommand(newSyncClusterCmd(app), newSyncFluxCmd(app))
	return c
}

func newSyncClusterCmd(app *cli.App) *cobra.Command {
	var branch string
	var force bool
	c := &cobra.Command{
		Use:   "cluster",
		Short: "Re-populate the GitOps repository from the embedded cluster template",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			r, err := resolve(app)
			if err != nil {
				return err
			}
			if branch != "" {
				r.Git.Branch = branch
			}
			if force && !app.Flags.DryRun {
				ok, err := app.Confirm(fmt.Sprintf("Force sync overwrites the contents of %s/%s and pushes to %s. Continue?", r.Git.Owner, r.Git.Repository, r.Git.Branch))
				if err != nil {
					return err
				}
				if !ok {
					app.IO.Errln("Aborted.")
					return nil
				}
			}
			if app.Flags.DryRun {
				mode := "skip if the cluster path exists"
				if force {
					mode = "force-overwrite the repository contents"
				}
				app.IO.Errf("Would sync the embedded cluster template into %s/%s (branch %s): %s.\n", r.Git.Owner, r.Git.Repository, r.Git.Branch, mode)
				return nil
			}
			ctx, cancel := app.Context(clusterWaitTimeout)
			defer cancel()
			if err := provisionAutoRepository(ctx, app, r); err != nil {
				return err
			}
			gm := gitops.New(runner(app), app.IO, r, fluxFor(app, r))
			return gm.SyncClusterTemplate(ctx, force)
		},
	}
	c.Flags().StringVar(&branch, "branch", "", "Override the GitOps branch")
	c.Flags().BoolVar(&force, "force", false, "Force the template sync even if the cluster path exists")
	return c
}

func newSyncFluxCmd(app *cli.App) *cobra.Command {
	return &cobra.Command{
		Use:   "flux",
		Short: "Refresh the FluxCD base manifests from the embedded cluster template",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			r, err := resolve(app)
			if err != nil {
				return err
			}
			ctx, cancel := app.Context(clusterWaitTimeout)
			defer cancel()
			gm := gitops.New(runner(app), app.IO, r, fluxFor(app, r))
			return gm.UpdateFlux(ctx)
		},
	}
}
