package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/micronisus/leptostack-cli/internal/ca"
	"github.com/micronisus/leptostack-cli/internal/cli"
	"github.com/micronisus/leptostack-cli/internal/config"
	"github.com/micronisus/leptostack-cli/internal/ui"
)

func newCACmd(app *cli.App) *cobra.Command {
	c := &cobra.Command{
		Use:   "ca",
		Short: "Manage the internal CA certificate",
		Args:  cobra.NoArgs,
		RunE:  func(cmd *cobra.Command, args []string) error { return cmd.Help() },
	}
	c.AddCommand(newCAStatusCmd(app), newCAInstallCmd(app), newCAExportCmd(app))
	return c
}

func caStore() ca.Store { return ca.Default(config.Dir()) }

type caStatus struct {
	Certificate bool   `json:"certificate"`
	PrivateKey  bool   `json:"privateKey"`
	Trusted     string `json:"trusted"`
	CertFile    string `json:"certFile"`
	KeyFile     string `json:"keyFile"`
}

func newCAStatusCmd(app *cli.App) *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Report whether the CA exists and is trusted",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := app.ValidateOutput(cli.OutputJSON, cli.OutputYAML); err != nil {
				return err
			}
			store := caStore()
			r, _ := app.Resolve()
			domain := ""
			if r != nil {
				domain = r.Domain
			}
			ctx, cancel := app.Context(defaultWaitTimeout)
			defer cancel()
			st := caStatus{
				Certificate: fileNonEmpty(store.CertFile),
				PrivateKey:  fileNonEmpty(store.KeyFile),
				Trusted:     store.TrustStatus(ctx, runner(app), distro(), domain),
				CertFile:    store.CertFile,
				KeyFile:     store.KeyFile,
			}
			return output(app, st, func() {
				color := app.Colorizer()
				t := ui.NewTable("FIELD", "VALUE")
				t.AddRow("certificate", fmt.Sprintf("%t", st.Certificate))
				t.AddRow("privateKey", fmt.Sprintf("%t", st.PrivateKey))
				t.AddRow("trusted", color.Status(st.Trusted))
				t.AddRow("certFile", st.CertFile)
				t.AddRow("keyFile", st.KeyFile)
				t.Render(app.IO.Out)
			})
		},
	}
}

func newCAInstallCmd(app *cli.App) *cobra.Command {
	return &cobra.Command{
		Use:     "install",
		Aliases: []string{"add-trust", "trust"},
		Short:   "Install the CA certificate into the system trust store",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			r, err := resolve(app)
			if err != nil {
				return err
			}
			store := caStore()
			if err := store.Ensure(); err != nil {
				return err
			}
			d := distro()
			if d == "" {
				return cli.Exitf(cli.ExitPrereq, "unsupported Linux distribution; CA install supports Fedora and Arch")
			}
			ctx, cancel := app.Context(defaultWaitTimeout)
			defer cancel()
			app.IO.Errln("Installing CA certificate to system trust store...")
			if err := store.Install(ctx, runner(app), d, r.Domain); err != nil {
				return err
			}
			app.IO.Errln("CA certificate installed successfully.\n\nPlease restart your browsers for the new CA certificate to take effect.")
			return nil
		},
	}
}

func newCAExportCmd(app *cli.App) *cobra.Command {
	var outFile string
	c := &cobra.Command{
		Use:   "export",
		Short: "Write the CA certificate PEM to stdout or a file",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			store := caStore()
			if !store.Exists() {
				if err := store.Ensure(); err != nil {
					return err
				}
			}
			pemBytes, err := store.PEM()
			if err != nil {
				return err
			}
			if outFile == "" {
				_, err := app.IO.Out.Write(pemBytes)
				return err
			}
			if err := os.WriteFile(outFile, pemBytes, 0o644); err != nil {
				return err
			}
			app.IO.Errf("Wrote %s\n", outFile)
			return nil
		},
	}
	c.Flags().StringVar(&outFile, "file", "", "Write the PEM to this file instead of stdout")
	return c
}

func fileNonEmpty(path string) bool {
	fi, err := os.Stat(path)
	return err == nil && fi.Size() > 0
}
