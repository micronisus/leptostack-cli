package cmd

import (
	"context"
	"time"

	"github.com/spf13/cobra"

	"github.com/micronisus/leptostack-cli/internal/cli"
	"github.com/micronisus/leptostack-cli/internal/kubernetes"
	"github.com/micronisus/leptostack-cli/internal/kustomizations"
)

func newWaitCmd(app *cli.App) *cobra.Command {
	c := &cobra.Command{
		Use:   "wait",
		Short: "Block on a condition with a timeout",
		Args:  cobra.NoArgs,
		RunE:  func(cmd *cobra.Command, args []string) error { return cmd.Help() },
	}
	c.AddCommand(newWaitKustomizationCmd(app))
	return c
}

func newWaitKustomizationCmd(app *cli.App) *cobra.Command {
	var ready bool
	c := &cobra.Command{
		Use:   "kustomization <name>|all",
		Short: "Wait for kustomization(s) to reach a ready state",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			r, err := resolve(app)
			if err != nil {
				return err
			}
			ctx, cancel := app.Context(defaultWaitTimeout)
			defer cancel()

			kube := kubeFor(app, r)
			if err := requireReachable(ctx, app, kube); err != nil {
				return err
			}

			timeout := app.Timeout(defaultWaitTimeout)

			names := []string{args[0]}
			if args[0] == "all" {
				names, err = kustomizations.Names(ctx, kube)
				if err != nil {
					return err
				}
				if len(names) == 0 {
					app.IO.Errln("No kustomizations found.")
					return nil
				}
			}

			for _, name := range names {
				if err := waitOne(ctx, app, kube, name, ready, timeout); err != nil {
					return err
				}
			}
			return nil
		},
	}
	c.Flags().BoolVar(&ready, "ready", true, "Wait until ready (use --ready=false to wait until not ready)")
	return c
}

func waitOne(ctx context.Context, app *cli.App, kube *kubernetes.Client, name string, wantReady bool, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		isReady := kustomizations.IsReady(ctx, kube, name)
		if isReady == wantReady {
			return nil
		}
		if time.Now().After(deadline) {
			return cli.Exitf(cli.ExitTimeout, "timed out after %s waiting for %s (ready=%t)", timeout, name, wantReady)
		}
		app.IO.Errf("  Waiting for %s (ready=%t)...\n", name, wantReady)
		select {
		case <-ctx.Done():
			return cli.Exitf(cli.ExitTimeout, "timed out waiting for %s", name)
		case <-time.After(10 * time.Second):
		}
	}
}
