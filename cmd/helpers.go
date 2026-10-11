package cmd

import (
	"context"
	"strings"
	"time"

	"github.com/micronisus/leptostack-cli/internal/ca"
	"github.com/micronisus/leptostack-cli/internal/cli"
	"github.com/micronisus/leptostack-cli/internal/config"
	"github.com/micronisus/leptostack-cli/internal/execx"
	"github.com/micronisus/leptostack-cli/internal/flux"
	"github.com/micronisus/leptostack-cli/internal/kubernetes"
	versionpkg "github.com/micronisus/leptostack-cli/internal/version"
)

// Default timeouts.
const (
	defaultWaitTimeout   = 30 * time.Minute
	defaultReconcileTime = 5 * time.Minute
	defaultReadyTimeout  = 30 * time.Minute
	clusterWaitTimeout   = 30 * time.Minute
)

func runner(app *cli.App) *execx.Runner {
	return execx.New(app.IO, app.Flags.Verbose)
}

// kubeFor builds a kubectl client for the resolved cluster.
func kubeFor(app *cli.App, r *config.Resolved) *kubernetes.Client {
	return kubernetes.New(runner(app), r.KubeContext, app.Flags.Kubeconfig, "")
}

// fluxFor builds a flux client for the resolved cluster.
func fluxFor(app *cli.App, r *config.Resolved) *flux.Client {
	return flux.New(runner(app), r.KubeContext, app.Flags.Kubeconfig)
}

// resolve loads config and resolves the selected cluster, mapping errors to the
// configuration exit code.
func resolve(app *cli.App) (*config.Resolved, error) {
	r, err := app.Resolve()
	if err != nil {
		return nil, err
	}
	return r, nil
}

// requireReachable fails with exit code 6 when the cluster API is unreachable.
func requireReachable(ctx context.Context, app *cli.App, kube *kubernetes.Client) error {
	if kube.Healthz(ctx) {
		return nil
	}
	return cli.Exitf(cli.ExitUnreachable, "cluster %s is not reachable; start it with 'leptostack start'", kube.KubeContext)
}

// distro detects the Linux distribution or returns "" on unsupported systems.
func distro() string {
	d, err := ca.DetectDistro()
	if err != nil {
		return ""
	}
	return d
}

// splitResourceArg parses `resource/name` or a bare name.
func splitResourceArg(arg string) (resource, name string) {
	if i := strings.IndexByte(arg, '/'); i >= 0 {
		return arg[:i], arg[i+1:]
	}
	return "", arg
}

// outOrHuman writes machine output for json/yaml, otherwise calls human.
func output(app *cli.App, v any, human func()) error {
	return app.EncodeOrHuman(v, human)
}

func execxLookPath(name string) (string, bool) { return execx.LookPath(name) }

func execxCmd(name string, args ...string) execx.Cmd {
	return execx.Cmd{Name: name, Args: args}
}

// firstVersion extracts the first dotted version token from command output.
func firstVersion(out string) string {
	for _, f := range strings.Fields(out) {
		f = strings.Trim(f, "(),")
		f = strings.TrimPrefix(f, "v")
		if looksVersion(f) {
			return f
		}
	}
	return strings.TrimSpace(out)
}

func looksVersion(s string) bool {
	parts := strings.Split(s, ".")
	if len(parts) != 3 {
		return false
	}
	for _, p := range parts {
		if p == "" {
			return false
		}
		for _, r := range p {
			if r < '0' || r > '9' {
				return false
			}
		}
	}
	return true
}

func versionAtLeast(v, min string) bool { return versionpkg.AtLeast(v, min) }
