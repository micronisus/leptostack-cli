package cmd

import (
	"github.com/spf13/cobra"

	"github.com/micronisus/leptostack-cli/internal/cli"
	"github.com/micronisus/leptostack-cli/internal/dns"
)

func newDNSManager(app *cli.App) *dns.Manager {
	return &dns.Manager{R: runner(app), Distro: distro()}
}

func newDNSCmd(app *cli.App) *cobra.Command {
	c := &cobra.Command{
		Use:   "dns",
		Short: "Manage local DNS for the LeptoStack domain",
		Args:  cobra.NoArgs,
		RunE:  func(cmd *cobra.Command, args []string) error { return cmd.Help() },
	}
	c.AddCommand(newDNSUpdateCmd(app), newDNSGetCmd(app), newDNSDeleteCmd(app))
	return c
}

func newDNSUpdateCmd(app *cli.App) *cobra.Command {
	return &cobra.Command{
		Use:     "update",
		Aliases: []string{"set"},
		Short:   "Configure local DNS for the LeptoStack domain",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			r, err := resolve(app)
			if err != nil {
				return err
			}
			ctx, cancel := app.Context(clusterWaitTimeout)
			defer cancel()
			kube := kubeFor(app, r)

			app.IO.Errln("Configuring DNS for " + r.Domain + "...")
			if err := newDNSManager(app).EnsurePlugin(ctx); err != nil {
				return err
			}

			target := gatewayIP(ctx, app, r, kube)
			if target == "" {
				return cli.Exitf(cli.ExitConfig, "could not determine the DNS target address for %s (apisix-gateway load-balancer IP)", r.Name)
			}
			if err := newDNSManager(app).Update(ctx, r.Name, r.Domain, target); err != nil {
				return err
			}
			app.IO.Errf("DNS configuration updated. *.%s now resolves to %s.\n", r.Domain, target)
			return nil
		},
	}
}

func newDNSGetCmd(app *cli.App) *cobra.Command {
	return &cobra.Command{
		Use:   "get",
		Short: "Print the effective dnsmasq rule",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := app.ValidateOutput(cli.OutputJSON, cli.OutputYAML); err != nil {
				return err
			}
			r, err := resolve(app)
			if err != nil {
				return err
			}
			rule, ok := newDNSManager(app).Get(r.Name)
			if app.IsMachine() {
				return app.Encode(map[string]any{"cluster": r.Name, "configured": ok, "rule": rule, "domain": r.Domain})
			}
			if !ok {
				app.IO.Errf("No DNS rule configured for cluster %s.\n", r.Name)
				return nil
			}
			app.IO.Println(rule)
			return nil
		},
	}
}

func newDNSDeleteCmd(app *cli.App) *cobra.Command {
	return &cobra.Command{
		Use:     "delete",
		Aliases: []string{"remove"},
		Short:   "Remove the DNS rule file",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			r, err := resolve(app)
			if err != nil {
				return err
			}
			ctx, cancel := app.Context(clusterWaitTimeout)
			defer cancel()
			if err := newDNSManager(app).Delete(ctx, r.Name); err != nil {
				return err
			}
			app.IO.Errf("DNS rule for cluster %s removed.\n", r.Name)
			return nil
		},
	}
}
