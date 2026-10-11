package cmd

import (
	"github.com/spf13/cobra"

	"github.com/micronisus/leptostack-cli/internal/cli"
)

func newCompletionCmd(app *cli.App) *cobra.Command {
	return &cobra.Command{
		Use:       "completion [bash|zsh|fish|powershell]",
		Short:     "Generate a shell completion script",
		Args:      cobra.ExactArgs(1),
		ValidArgs: []string{"bash", "zsh", "fish", "powershell"},
		RunE: func(cmd *cobra.Command, args []string) error {
			root := cmd.Root()
			switch args[0] {
			case "bash":
				return root.GenBashCompletionV2(app.IO.Out, true)
			case "zsh":
				return root.GenZshCompletion(app.IO.Out)
			case "fish":
				return root.GenFishCompletion(app.IO.Out, true)
			case "powershell":
				return root.GenPowerShellCompletionWithDesc(app.IO.Out)
			default:
				return cli.Exitf(cli.ExitUsage, "unsupported shell %q (expected bash, zsh, fish or powershell)", args[0])
			}
		},
	}
}
