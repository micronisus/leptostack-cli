package cmd

import (
	"io"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/micronisus/leptostack-cli/internal/cli"
	"github.com/micronisus/leptostack-cli/internal/runtime"
)

func newLogsCmd(app *cli.App) *cobra.Command {
	c := &cobra.Command{
		Use:   "logs",
		Short: "Show logs for a background process",
		Args:  cobra.NoArgs,
		RunE:  func(cmd *cobra.Command, args []string) error { return cmd.Help() },
	}
	c.AddCommand(newLogsPortForwardCmd(app))
	return c
}

func newLogsPortForwardCmd(app *cli.App) *cobra.Command {
	var follow bool
	c := &cobra.Command{
		Use:   "port-forward <service>",
		Short: "Show the log for a background port-forward",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			r, err := resolve(app)
			if err != nil {
				return err
			}
			path := runtime.PortForwardLogFile(r.Name, args[0])
			f, err := os.Open(path)
			if err != nil {
				return cli.Exitf(cli.ExitFailure, "no log for port-forward %s (expected %s)", args[0], path)
			}
			defer f.Close()

			if _, err := io.Copy(app.IO.Out, f); err != nil {
				return err
			}
			if !follow {
				return nil
			}
			return followFile(cmd, app, f)
		},
	}
	c.Flags().BoolVarP(&follow, "follow", "f", false, "Follow the log output")
	return c
}

func followFile(cmd *cobra.Command, app *cli.App, f *os.File) error {
	buf := make([]byte, 4096)
	for {
		n, err := f.Read(buf)
		if n > 0 {
			if _, werr := app.IO.Out.Write(buf[:n]); werr != nil {
				return werr
			}
		}
		if err == io.EOF {
			select {
			case <-cmd.Context().Done():
				return nil
			case <-time.After(500 * time.Millisecond):
			}
			// Re-check for truncation or new content.
			if fi, serr := f.Stat(); serr == nil {
				pos, _ := f.Seek(0, io.SeekCurrent)
				if pos > fi.Size() {
					_, _ = f.Seek(0, io.SeekStart)
				}
			}
			continue
		}
		if err != nil {
			return err
		}
	}
}
