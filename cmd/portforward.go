package cmd

import (
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/micronisus/leptostack-cli/internal/cli"
	"github.com/micronisus/leptostack-cli/internal/config"
	"github.com/micronisus/leptostack-cli/internal/kubernetes"
	"github.com/micronisus/leptostack-cli/internal/portforward"
	"github.com/micronisus/leptostack-cli/internal/runtime"
)

type portForwardOptions struct {
	detach           bool
	foreground       bool
	localPort        int
	address          string
	internalDetached bool
}

func newPortForwardCmd(app *cli.App) *cobra.Command {
	var opts portForwardOptions
	var localPortStr string
	c := &cobra.Command{
		Use:   "port-forward <service>",
		Short: "Forward a local port to a cluster service (foreground by default)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			lp, err := portforward.ParseLocalPort(localPortStr)
			if err != nil {
				return cli.Exitf(cli.ExitUsage, "%v", err)
			}
			opts.localPort = lp
			if opts.detach && opts.foreground {
				return cli.Exitf(cli.ExitUsage, "--detach and --foreground are mutually exclusive")
			}
			return runPortForward(cmd, app, args[0], opts)
		},
	}
	f := c.Flags()
	f.BoolVarP(&opts.detach, "detach", "d", false, "Run the port-forward in the background")
	f.BoolVarP(&opts.foreground, "foreground", "f", false, "Run in the foreground (default)")
	f.StringVar(&localPortStr, "local-port", "", "Override the local port")
	f.StringVar(&opts.address, "address", "", "Local address to bind (default: 127.0.0.1)")
	f.BoolVar(&opts.internalDetached, "internal-detached", false, "internal: run as the detached child")
	_ = f.MarkHidden("internal-detached")
	return c
}

func runPortForward(cmd *cobra.Command, app *cli.App, service string, opts portForwardOptions) error {
	r, err := resolve(app)
	if err != nil {
		return err
	}

	if opts.internalDetached {
		return runDetachedChild(app, r, service, opts)
	}

	if service == "all" {
		for _, s := range portforward.Catalog(r) {
			if err := startDetached(app, r, s.Name, opts); err != nil {
				return err
			}
		}
		return nil
	}

	svc, err := portforward.Lookup(r, service)
	if err != nil {
		return cli.Exitf(cli.ExitUsage, "%v", err)
	}
	if opts.detach {
		return startDetached(app, r, service, opts)
	}

	ctx, cancel := app.Context(0)
	defer cancel()
	kube := kubeFor(app, r)
	if err := requireReachable(ctx, app, kube); err != nil {
		return err
	}
	printPortForwardCredentials(ctx, app, kube, r, svc.Name)
	app.IO.Errf("Forwarding %s (Ctrl-C to stop)...\n", service)
	return kube.Run(ctx, svc.PortForwardArgs(opts.address, opts.localPort)...)
}

func startDetached(app *cli.App, r *config.Resolved, service string, opts portForwardOptions) error {
	svc, err := portforward.Lookup(r, service)
	if err != nil {
		return cli.Exitf(cli.ExitUsage, "%v", err)
	}

	exe, err := os.Executable()
	if err != nil {
		return err
	}
	args := []string{"port-forward", service, "--internal-detached", "--cluster", r.Name}
	if app.Flags.Context != "" {
		args = append(args, "--context", app.Flags.Context)
	}
	if app.Flags.Kubeconfig != "" {
		args = append(args, "--kubeconfig", app.Flags.Kubeconfig)
	}
	if opts.localPort > 0 {
		args = append(args, "--local-port", fmt.Sprintf("%d", opts.localPort))
	}
	if opts.address != "" {
		args = append(args, "--address", opts.address)
	}

	if _, err := runtime.EnsureClusterDir(r.Name); err != nil {
		return err
	}
	if err := os.MkdirAll(runtime.PortForwardDir(r.Name), 0o700); err != nil {
		return err
	}
	logFile := runtime.PortForwardLogFile(r.Name, service)
	lf, err := os.OpenFile(logFile, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	defer lf.Close()

	child := exec.Command(exe, args...)
	child.Stdout = lf
	child.Stderr = lf
	child.Stdin = nil
	portforward.ConfigureDetached(child)
	if err := child.Start(); err != nil {
		return err
	}
	_ = child.Process.Release()

	// Wait briefly for the child to publish its state file.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if st, _ := portforward.ReadState(r.Name, service); st != nil {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}

	printPortForwardCredentials(context.Background(), app, kubeFor(app, r), r, svc.Name)
	app.IO.Errf("Port-forward for %s started in the background.\nLogs: %s\n", service, logFile)
	return nil
}

func runDetachedChild(app *cli.App, r *config.Resolved, service string, opts portForwardOptions) error {
	svc, err := portforward.Lookup(r, service)
	if err != nil {
		return err
	}
	if _, err := runtime.EnsureClusterDir(r.Name); err != nil {
		return err
	}
	mappings := make([]portforward.Mapping, len(svc.Mappings))
	copy(mappings, svc.Mappings)
	if opts.localPort > 0 && len(mappings) > 0 {
		mappings[0].Local = opts.localPort
	}

	logFile := runtime.PortForwardLogFile(r.Name, service)
	st := portforward.NewState(r.Name, service, r.KubeContext, logFile, mappings)
	if err := portforward.WriteState(r.Name, st); err != nil {
		return err
	}
	defer portforward.RemoveState(r.Name, service)

	ctx := context.Background()
	kube := kubeFor(app, r)
	return kube.Run(ctx, svc.PortForwardArgs(opts.address, opts.localPort)...)
}

func printPortForwardCredentials(ctx context.Context, app *cli.App, kube *kubernetes.Client, r *config.Resolved, service string) {
	c := r.ClusterName
	switch service {
	case "openbao":
		if token, err := kube.Output(ctx, "-n", c+"-openbao", "get", "secrets", "openbao-init", "-o", "jsonpath={.data.root_token}"); err == nil {
			app.IO.Errf("OpenBao root token: %s\n", decode64(token))
		}
	case "rabbitmq":
		ns := c + "-rabbitmq"
		u, _ := kube.Output(ctx, "-n", ns, "get", "secret", c+"-rabbitmq-default-user", "-o", "jsonpath={.data.username}")
		p, _ := kube.Output(ctx, "-n", ns, "get", "secret", c+"-rabbitmq-default-user", "-o", "jsonpath={.data.password}")
		app.IO.Errf("RabbitMQ credentials:\n  Username: %s\n  Password: %s\n", decode64(u), decode64(p))
	case "postgres":
		ns := c + "-pgcluster"
		u, _ := kube.Output(ctx, "-n", ns, "get", "secret", c+"-pgcluster-superuser", "-o", "jsonpath={.data.username}")
		p, _ := kube.Output(ctx, "-n", ns, "get", "secret", c+"-pgcluster-superuser", "-o", "jsonpath={.data.password}")
		app.IO.Errf("PostgreSQL superuser credentials:\n  Username: %s\n  Password: %s\n", decode64(u), decode64(p))
	case "valkey":
		p, _ := kube.Output(ctx, "-n", c+"-valkey", "get", "secret", "valkey-admin", "-o", "jsonpath={.data.password}")
		app.IO.Errf("Valkey credentials:\n  Password: %s\n", decode64(p))
	case "rustfs":
		ns := c + "-rustfs"
		ak, _ := kube.Output(ctx, "-n", ns, "get", "secret", "rustfs-credentials", "-o", "jsonpath={.data.RUSTFS_ACCESS_KEY}")
		sk, _ := kube.Output(ctx, "-n", ns, "get", "secret", "rustfs-credentials", "-o", "jsonpath={.data.RUSTFS_SECRET_KEY}")
		app.IO.Errf("RustFS S3 credentials:\n  Access key: %s\n  Secret key: %s\n", decode64(ak), decode64(sk))
	}
}

func decode64(s string) string {
	s = strings.TrimSpace(s)
	b, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		return s
	}
	return string(b)
}
