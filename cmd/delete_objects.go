package cmd

import (
	"fmt"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/micronisus/leptostack-cli/internal/cli"
	"github.com/micronisus/leptostack-cli/internal/gitops"
	"github.com/micronisus/leptostack-cli/internal/portforward"
	"github.com/micronisus/leptostack-cli/internal/sources"
)

func newDeleteSourceCmd(app *cli.App) *cobra.Command {
	var toGit, direct bool
	c := &cobra.Command{
		Use:   "source <git|helm|bucket|oci>/<name>",
		Short: "Delete a Flux source",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			typ, name := splitResourceArg(args[0])
			if typ == "" {
				return cli.Exitf(cli.ExitUsage, "specify the source type, e.g. %s/%s", sources.TypeGit, args[0])
			}
			kind, err := sources.Kind(typ)
			if err != nil {
				return cli.Exitf(cli.ExitUsage, "%v", err)
			}
			if toGit && direct {
				return cli.Exitf(cli.ExitUsage, "--git and --direct are mutually exclusive")
			}
			r, err := resolve(app)
			if err != nil {
				return err
			}
			if app.Flags.DryRun {
				if toGit {
					app.IO.Errf("Would remove source %s from the GitOps repository.\n", name)
				} else {
					app.IO.Errf("Would delete source %s (%s) from the cluster.\n", name, typ)
				}
				return nil
			}
			ok, err := app.Confirm(fmt.Sprintf("Delete %s source %s?", typ, name))
			if err != nil {
				return err
			}
			if !ok {
				app.IO.Errln("Aborted.")
				return nil
			}
			ctx, cancel := app.Context(clusterWaitTimeout)
			defer cancel()

			if toGit {
				gm := gitops.New(runner(app), app.IO, r, fluxFor(app, r))
				relPath := filepath.ToSlash(filepath.Join(r.ClonePath(), "sources", typ, name+".yaml"))
				if err := gm.RemoveFile(ctx, relPath, fmt.Sprintf("Remove %s source %s", typ, name)); err != nil {
					return err
				}
				app.IO.Errf("Source %s removed from the repository.\n", name)
				return nil
			}

			ns := defaultNamespace(app)
			if err := kubeFor(app, r).RunDiscard(ctx, "delete", kind, name, "-n", ns, "--ignore-not-found"); err != nil {
				return err
			}
			app.IO.Errf("Source %s (%s) deleted from the cluster.\n", name, typ)
			return nil
		},
	}
	c.Flags().BoolVar(&toGit, "git", false, "Remove the manifest from the GitOps repository")
	c.Flags().BoolVar(&direct, "direct", false, "Delete from the cluster (default)")
	return c
}

func newDeleteSecretCmd(app *cli.App) *cobra.Command {
	var namespace string
	c := &cobra.Command{
		Use:   "secret <name>",
		Short: "Delete a Kubernetes secret",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			r, err := resolve(app)
			if err != nil {
				return err
			}
			if namespace == "" {
				namespace = defaultNamespace(app)
			}
			if app.Flags.DryRun {
				app.IO.Errf("Would delete secret %s from namespace %s.\n", args[0], namespace)
				return nil
			}
			ok, err := app.Confirm(fmt.Sprintf("Delete secret %s from namespace %s?", args[0], namespace))
			if err != nil {
				return err
			}
			if !ok {
				app.IO.Errln("Aborted.")
				return nil
			}
			ctx, cancel := app.Context(clusterWaitTimeout)
			defer cancel()
			if err := kubeFor(app, r).RunDiscard(ctx, "delete", "secret", args[0], "-n", namespace, "--ignore-not-found"); err != nil {
				return err
			}
			app.IO.Errf("Secret %s deleted from namespace %s.\n", args[0], namespace)
			return nil
		},
	}
	c.Flags().StringVarP(&namespace, "namespace", "n", "", "Namespace (default: flux-system for the current cluster)")
	return c
}

func newDeletePortForwardCmd(app *cli.App) *cobra.Command {
	var all bool
	c := &cobra.Command{
		Use:   "port-forward <service>",
		Short: "Stop a background port-forward",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			r, err := resolve(app)
			if err != nil {
				return err
			}
			if !all && len(args) == 0 {
				return cli.Exitf(cli.ExitUsage, "specify a service or --all")
			}
			stopped := 0
			if all {
				states, err := portforward.ListStates(r.Name)
				if err != nil {
					return err
				}
				for i := range states {
					if states[i].Alive() {
						_ = states[i].Kill()
						app.IO.Errf("Stopped port-forward for %s (pid %d).\n", states[i].Service, states[i].PID)
						stopped++
					}
					portforward.RemoveState(r.Name, states[i].Service)
				}
				stopped += portforward.LegacyStopForwards()
			} else {
				svc := args[0]
				st, err := portforward.ReadState(r.Name, svc)
				if err != nil {
					return err
				}
				if st != nil && st.Alive() {
					_ = st.Kill()
					app.IO.Errf("Stopped port-forward for %s (pid %d).\n", svc, st.PID)
					stopped++
				}
				portforward.RemoveState(r.Name, svc)
			}
			if stopped == 0 {
				app.IO.Errln("No active port-forwards.")
			}
			return nil
		},
	}
	c.Flags().BoolVar(&all, "all", false, "Stop all port-forwards for the cluster")
	return c
}
