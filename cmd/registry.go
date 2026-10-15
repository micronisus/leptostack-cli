package cmd

import (
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/micronisus/leptostack-cli/internal/cli"
	"github.com/micronisus/leptostack-cli/internal/config"
	"github.com/micronisus/leptostack-cli/internal/registry"
	"github.com/micronisus/leptostack-cli/internal/ui"
)

func newRegistryCmd(app *cli.App) *cobra.Command {
	c := &cobra.Command{
		Use:   "registry",
		Short: "Manage module image registry credentials",
		Args:  cobra.NoArgs,
		RunE:  func(cmd *cobra.Command, args []string) error { return cmd.Help() },
	}
	c.AddCommand(newRegistryGetCmd(app), newRegistrySetCmd(app), newRegistryDeleteCmd(app))
	return c
}

func newRegistryGetCmd(app *cli.App) *cobra.Command {
	return &cobra.Command{
		Use:   "get",
		Short: "Show the registry repository and credential source (redacted)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := app.ValidateOutput(cli.OutputJSON, cli.OutputYAML); err != nil {
				return err
			}
			r, err := resolve(app)
			if err != nil {
				return err
			}
			source := "none"
			switch {
			case os.Getenv(config.EnvRegistryCredentials) != "":
				source = "env"
			case r.Registry.Credentials != "":
				source = "config"
			}
			info := map[string]any{
				"repository": r.Registry.Repository,
				"configured": r.Registry.Credentials != "",
				"source":     source,
			}
			return output(app, info, func() {
				t := ui.NewTable("FIELD", "VALUE")
				t.AddRow("repository", r.Registry.Repository)
				t.AddRow("configured", yesNo(r.Registry.Credentials != ""))
				t.AddRow("source", source)
				t.Render(app.IO.Out)
			})
		},
	}
}

func newRegistrySetCmd(app *cli.App) *cobra.Command {
	var repo, user string
	var passwordStdin bool
	var password string
	c := &cobra.Command{
		Use:     "set-credentials",
		Aliases: []string{"set-registry-creds"},
		Short:   "Push registry credentials to OpenBao",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			r, err := resolve(app)
			if err != nil {
				return err
			}
			creds := r.Registry.Credentials
			repository := r.Registry.Repository

			if passwordStdin {
				pw, err := app.IO.ReadSecret()
				if err != nil {
					return err
				}
				password = strings.TrimSpace(pw)
			}
			if user != "" || password != "" {
				if user == "" || password == "" {
					return cli.Exitf(cli.ExitUsage, "--user and --password/--password-stdin must be provided together")
				}
				creds = user + ":" + password
			}
			if repo != "" {
				repository = repo
			}
			if repository == "" || creds == "" {
				return cli.Exitf(cli.ExitConfig, "registry repository and credentials are not configured")
			}
			if !strings.Contains(creds, ":") {
				return cli.Exitf(cli.ExitUsage, "registry credentials must be in user:pass format")
			}

			if repo != "" || user != "" || password != "" {
				cfg, err := app.Config()
				if err != nil {
					return err
				}
				cl := getOrCreateCluster(cfg, r.Name, r.ClusterName, r.Provider)
				reg := ensureClusterRegistry(cl)
				reg.Repository = repository
				reg.Credentials = creds
				if err := cfg.Save(); err != nil {
					return cli.MapConfigError(err)
				}
				app.IO.Errf("Registry credentials updated in %s.\n", config.DefaultPath())
			}

			r.Registry.Repository = repository
			r.Registry.Credentials = creds

			ctx, cancel := app.Context(clusterWaitTimeout)
			defer cancel()
			kube := kubeFor(app, r)
			if err := requireReachable(ctx, app, kube); err != nil {
				return err
			}
			m := registry.New(kube, app.IO, r.ClusterName)
			return m.Push(ctx, repository, creds)
		},
	}
	c.Flags().StringVar(&repo, "repo", "", "Registry repository")
	c.Flags().StringVar(&user, "user", "", "Registry username")
	c.Flags().BoolVar(&passwordStdin, "password-stdin", false, "Read the password from stdin")
	c.Flags().StringVar(&password, "password", "", "Registry password")
	_ = password
	return c
}

func newRegistryDeleteCmd(app *cli.App) *cobra.Command {
	return &cobra.Command{
		Use:   "delete",
		Short: "Remove registry credentials from OpenBao",
		Args:  cobra.NoArgs,
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
			m := registry.New(kube, app.IO, r.ClusterName)
			return m.Remove(ctx)
		},
	}
}

func yesNo(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}
