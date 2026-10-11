package cmd

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/micronisus/leptostack-cli/internal/cli"
	"github.com/micronisus/leptostack-cli/internal/config"
	"github.com/micronisus/leptostack-cli/internal/ui"
	"github.com/micronisus/leptostack-cli/internal/upgrade"
)

func newVersionCmd(app *cli.App) *cobra.Command {
	var check bool
	c := &cobra.Command{
		Use:   "version",
		Short: "Show the leptostack version",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			info := app.BuildInfo
			if err := app.ValidateOutput(cli.OutputJSON, cli.OutputYAML); err != nil {
				return err
			}
			if app.IsMachine() {
				return app.Encode(info)
			}
			app.IO.Printf("leptostack %s\n", info.Version)
			if app.Flags.Verbose > 0 {
				app.IO.Printf("  commit:   %s\n  built:    %s\n  go:       %s\n  platform: %s\n",
					info.Commit, info.Date, info.GoVersion, info.Platform)
			}
			if check {
				return runVersionCheck(cmd, app)
			}
			return nil
		},
	}
	c.Flags().BoolVar(&check, "check", false, "Check for a newer release")
	return c
}

func runVersionCheck(cmd *cobra.Command, app *cli.App) error {
	ctx, cancel := app.Context(defaultWaitTimeout)
	defer cancel()
	latest, available, err := upgrade.Check(ctx, config.Dir(), app.BuildInfo.Version, true)
	if err != nil {
		app.IO.Errf("Warning: could not check for updates: %v\n", err)
		return nil
	}
	if !available {
		app.IO.Errf("leptostack %s is the latest version.\n", app.BuildInfo.Version)
		return nil
	}
	app.IO.Errf("\nA newer version is available: v%s (current %s).\nUpdate with: leptostack upgrade\n", latest, app.BuildInfo.Version)
	return nil
}

func newUpgradeCmd(app *cli.App) *cobra.Command {
	var check bool
	c := &cobra.Command{
		Use:   "upgrade",
		Short: "Upgrade leptostack from GitHub releases",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, cancel := app.Context(defaultWaitTimeout)
			defer cancel()
			if check {
				return runVersionCheck(cmd, app)
			}
			return upgrade.Run(ctx, app.IO, app.BuildInfo.Version)
		},
	}
	c.Flags().BoolVar(&check, "check", false, "Only check for a newer release")
	return c
}

// prereq describes one external tool check.
type prereq struct {
	Name     string `json:"name"`
	Required bool   `json:"required"`
	Found    bool   `json:"found"`
	Version  string `json:"version,omitempty"`
	Min      string `json:"min,omitempty"`
	OK       bool   `json:"ok"`
	Message  string `json:"message,omitempty"`
}

func newDoctorCmd(app *cli.App) *cobra.Command {
	c := &cobra.Command{
		Use:   "doctor",
		Short: "Check prerequisite tools and cluster health",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, cancel := app.Context(defaultWaitTimeout)
			defer cancel()
			if err := app.ValidateOutput(cli.OutputJSON, cli.OutputYAML); err != nil {
				return err
			}

			checks := []prereq{
				checkTool(ctx, app, "kubectl", "", nil),
				checkTool(ctx, app, "flux", "2.8.7", fluxVersionArgs),
				checkTool(ctx, app, "git", "", nil),
			}

			provider := "minikube"
			if app.Flags.Cluster != "" || clusterConfigured(app) {
				if r, err := app.Resolve(); err == nil {
					provider = r.Provider
				}
			}
			if provider == "vcluster" {
				checks = append(checks, checkTool(ctx, app, "vcluster", "0.37.1", vclusterVersionArgs))
			} else {
				checks = append(checks, checkTool(ctx, app, "minikube", "1.38.1", minikubeVersionArgs))
				checks = append(checks, checkTool(ctx, app, "podman", "", nil))
			}

			allOK := true
			for _, p := range checks {
				if p.Required && !p.OK {
					allOK = false
				}
			}

			if app.IsMachine() {
				if err := app.Encode(checks); err != nil {
					return err
				}
			} else {
				color := app.Colorizer()
				t := ui.NewTable("TOOL", "STATUS", "VERSION", "MESSAGE")
				for _, p := range checks {
					status := color.Green("OK")
					if !p.OK {
						status = color.Red("FAIL")
					}
					t.AddRow(p.Name, status, p.Version, p.Message)
				}
				t.Render(app.IO.Out)
			}

			if !allOK {
				return cli.Exitf(cli.ExitPrereq, "one or more prerequisites are missing or too old")
			}
			return nil
		},
	}
	return c
}

func clusterConfigured(app *cli.App) bool {
	_, err := app.Resolve()
	return err == nil
}

var (
	fluxVersionArgs     = []string{"-v"}
	minikubeVersionArgs = []string{"version", "--short"}
	vclusterVersionArgs = []string{"--version"}
)

func checkTool(ctx context.Context, app *cli.App, name, min string, versionArgs []string) prereq {
	p := prereq{Name: name, Required: true}
	if _, ok := execxLookPath(name); !ok {
		p.Message = "not found on PATH"
		return p
	}
	p.Found = true
	if versionArgs == nil {
		p.Version = "unknown"
		p.OK = true
		return p
	}
	out, err := runner(app).OutputCombined(ctx, execxCmd(name, versionArgs...))
	if err != nil {
		p.Message = "could not determine version"
		return p
	}
	p.Version = firstVersion(out)
	p.Min = min
	if min == "" || versionAtLeast(p.Version, min) {
		p.OK = true
	} else {
		p.Message = fmt.Sprintf("requires >= %s", min)
	}
	return p
}
