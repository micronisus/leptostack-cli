package cmd

import (
	"os"

	"github.com/spf13/cobra"

	"github.com/micronisus/leptostack-cli/internal/cli"
	"github.com/micronisus/leptostack-cli/internal/clustertemplate"
)

func newGenerateCmd(app *cli.App) *cobra.Command {
	c := &cobra.Command{
		Use:   "generate",
		Short: "Generate files from an embedded template",
		Args:  cobra.NoArgs,
		RunE:  func(cmd *cobra.Command, args []string) error { return cmd.Help() },
	}
	c.AddCommand(newGenerateFluxCDCmd(app))
	return c
}

func newGenerateFluxCDCmd(app *cli.App) *cobra.Command {
	var force bool
	c := &cobra.Command{
		Use:   "fluxcd [dir]",
		Short: "Export the embedded FluxCD repository template into a directory",
		Long: `Export the embedded FluxCD repository template (clusters/, apps/,
infrastructure/) into a directory, defaulting to the current one. The command is
purely local: it needs neither a configuration nor a cluster.

The template is written as-is, so the cluster overlay stays under
clusters/example and can be renamed and customized afterwards. An existing
non-empty directory is left untouched unless --force is given.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			dst := "."
			if len(args) == 1 {
				dst = args[0]
			}
			if !force {
				empty, err := dirEmpty(dst)
				if err != nil {
					return err
				}
				if !empty {
					return cli.Exitf(cli.ExitUsage, "destination %s is not empty; pass --force to write the template into it", dst)
				}
			}
			if app.Flags.DryRun {
				app.IO.Errf("Would export the embedded FluxCD repository template into %q.\n", dst)
				return nil
			}
			if err := clustertemplate.Extract(dst); err != nil {
				return cli.Exitf(cli.ExitFailure, "could not write the template into %q: %v", dst, err)
			}
			app.IO.Errf("Exported the FluxCD repository template into %q.\n", dst)
			return nil
		},
	}
	c.Flags().BoolVar(&force, "force", false, "Write into the destination even when it is not empty")
	return c
}

// dirEmpty reports whether dir is missing or contains no entries.
func dirEmpty(dir string) (bool, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return true, nil
		}
		return false, err
	}
	return len(entries) == 0, nil
}
