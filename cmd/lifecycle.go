package cmd

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/micronisus/leptostack-cli/internal/ca"
	"github.com/micronisus/leptostack-cli/internal/cli"
	"github.com/micronisus/leptostack-cli/internal/config"
	"github.com/micronisus/leptostack-cli/internal/gitops"
	"github.com/micronisus/leptostack-cli/internal/kubernetes"
	"github.com/micronisus/leptostack-cli/internal/kustomizations"
	"github.com/micronisus/leptostack-cli/internal/portforward"
	"github.com/micronisus/leptostack-cli/internal/providers"
	"github.com/micronisus/leptostack-cli/internal/registry"
	"github.com/micronisus/leptostack-cli/internal/secrets"
	"github.com/micronisus/leptostack-cli/internal/ui"
)

func providerDeps(app *cli.App, r *config.Resolved) providers.Deps {
	return providers.Deps{
		R:          runner(app),
		IO:         app.IO,
		Resolved:   r,
		Kube:       kubeFor(app, r),
		Flux:       fluxFor(app, r),
		CA:         ca.Default(config.Dir()),
		Distro:     distro(),
		Kubeconfig: app.Flags.Kubeconfig,
	}
}

func newStartCmd(app *cli.App) *cobra.Command {
	var wait bool
	c := &cobra.Command{
		Use:   "start",
		Short: "Start the cluster and bootstrap GitOps",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			r, err := resolve(app)
			if err != nil {
				return err
			}
			ctx, cancel := app.Context(clusterWaitTimeout)
			defer cancel()
			return runStart(ctx, app, r, wait)
		},
	}
	c.Flags().BoolVar(&wait, "wait", false, "Wait for all kustomizations to become ready before returning")
	return c
}

func runStart(ctx context.Context, app *cli.App, r *config.Resolved, wait bool) error {
	app.IO.Errln("=== Starting LeptoStack Development Environment ===")
	app.IO.Errln()

	store := ca.Default(config.Dir())
	if err := store.Ensure(); err != nil {
		return err
	}
	app.IO.Errln()

	provider := providers.New(providerDeps(app, r))
	if err := provider.Start(ctx); err != nil {
		return err
	}

	kube := kubeFor(app, r)
	if !kubernetes.ContextMatches(ctx, runner(app), app.Flags.Kubeconfig, r.KubeContext) {
		return cli.Exitf(cli.ExitFailure, "kubectl context is not set to %s; run 'leptostack start' again", r.KubeContext)
	}

	// Phase 2: create the GitOps repository and Flux tokens when configured as "auto".
	if err := provisionAutoRepository(ctx, app, r); err != nil {
		return err
	}

	if err := ensureFluxBootstrapped(ctx, app, r, kube); err != nil {
		return err
	}

	if err := createLeptostackBaseSecret(ctx, app, r, kube); err != nil {
		return err
	}
	if err := applyCASecret(ctx, app, r, kube, store); err != nil {
		return err
	}
	if err := uploadRegistryCredentials(ctx, app, r, kube); err != nil {
		return err
	}
	if wait {
		return waitAllKustomizations(ctx, app, kube)
	}
	return nil
}

// waitAllKustomizations blocks until every kustomization in flux-system is
// ready or the command timeout elapses.
func waitAllKustomizations(ctx context.Context, app *cli.App, kube *kubernetes.Client) error {
	names, err := kustomizations.Names(ctx, kube)
	if err != nil {
		return err
	}
	timeout := app.Timeout(defaultWaitTimeout)
	for _, name := range names {
		if err := kustomizations.WaitReady(ctx, kube, name, timeout, progressLogger(app, name)); err != nil {
			return cli.Exitf(cli.ExitTimeout, "%v", err)
		}
	}
	return nil
}

func ensureFluxBootstrapped(ctx context.Context, app *cli.App, r *config.Resolved, kube *kubernetes.Client) error {
	if _, err := kube.OutputCombined(ctx, "get", "deployment", "source-controller", "-n", "flux-system"); err == nil {
		app.IO.Errln("Flux is already bootstrapped, skipping bootstrap.")
		app.IO.Errln("You can check the status of the deployment by running:\n  flux get kustomizations")
		return nil
	}
	gm := gitops.New(runner(app), app.IO, r, fluxFor(app, r))
	return gm.RunBootstrap(ctx, false)
}

func createLeptostackBaseSecret(ctx context.Context, app *cli.App, r *config.Resolved, kube *kubernetes.Client) error {
	app.IO.Errln("\nCreating leptostack-base secret in flux-system namespace...")
	pat := r.Base.PAT
	if pat == "" {
		pat = r.Git.PAT
	}
	data := map[string]string{
		"username": "git",
		"password": pat,
	}
	// A git server behind an enterprise CA is validated with a ca.crt entry in
	// the same secret the base source already references.
	if r.Git.CAFile != "" {
		caBundle, err := os.ReadFile(r.Git.CAFile)
		if err != nil {
			return cli.Exitf(cli.ExitFailure, "could not read the git CA file %q: %v", r.Git.CAFile, err)
		}
		data["ca.crt"] = string(caBundle)
	}
	// A CA dedicated to the leptostack-base repository is stored under the
	// caFile key in the same secret.
	if r.Base.CAFile != "" {
		caBundle, err := os.ReadFile(r.Base.CAFile)
		if err != nil {
			return cli.Exitf(cli.ExitFailure, "could not read the leptostack-base CA file %q: %v", r.Base.CAFile, err)
		}
		data["caFile"] = string(caBundle)
	}
	if _, err := kube.OutputCombined(ctx, "get", "secret", "leptostack-base", "-n", "flux-system"); err == nil {
		app.IO.Errln("  Secret leptostack-base already exists, updating...")
		_ = kube.RunDiscard(ctx, "delete", "secret", "leptostack-base", "-n", "flux-system")
	}
	manifest := secrets.ManifestStringData("leptostack-base", "flux-system", "Opaque", data)
	if err := kube.ApplyJSON(ctx, manifest); err != nil {
		return err
	}
	app.IO.Errln("  Secret leptostack-base created successfully.")
	return nil
}

func applyCASecret(ctx context.Context, app *cli.App, r *config.Resolved, kube *kubernetes.Client, store ca.Store) error {
	if err := kustomizations.WaitReady(ctx, kube, "cert-manager", app.Timeout(defaultReadyTimeout), progressLogger(app, "cert-manager")); err != nil {
		return err
	}

	if err := ensureCertificateRemoved(ctx, app, kube); err != nil {
		return err
	}
	// Drop any cert-manager-generated secret before installing the provided CA.
	_ = kube.RunDiscard(ctx, "delete", "secret", "internal-ca-secret", "-n", "cert-manager", "--ignore-not-found")

	app.IO.Errln("Installing the provided CA as the internal-ca-secret TLS secret in the cert-manager namespace...")
	dry, err := kube.Output(ctx,
		"create", "secret", "tls", "internal-ca-secret",
		"--cert="+store.CertFile, "--key="+store.KeyFile,
		"-n", "cert-manager", "--dry-run=client", "-o", "yaml")
	if err != nil {
		return err
	}
	if err := kube.ApplyJSON(ctx, dry); err != nil {
		return err
	}
	app.IO.Errln("  Secret internal-ca-secret applied.")
	return nil
}

func ensureCertificateRemoved(ctx context.Context, app *cli.App, kube *kubernetes.Client) error {
	for attempt := 0; attempt < 6; attempt++ {
		if _, err := kube.OutputCombined(ctx, "get", "certificate", "internal-ca", "-n", "cert-manager"); err != nil {
			return nil // gone
		}
		_ = kube.RunDiscard(ctx, "delete", "certificate", "internal-ca", "-n", "cert-manager", "--ignore-not-found")
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(3 * time.Second):
		}
	}
	return cli.Exitf(cli.ExitFailure,
		"certificate cert-manager/internal-ca keeps being recreated: the infra-config delete patch is missing from the GitOps repository; run 'leptostack sync cluster --force' and retry")
}

func uploadRegistryCredentials(ctx context.Context, app *cli.App, r *config.Resolved, kube *kubernetes.Client) error {
	if r.Registry.Repository == "" || r.Registry.Credentials == "" {
		return nil
	}
	if err := kustomizations.WaitReady(ctx, kube, r.ClusterName+"-openbao-post", app.Timeout(defaultReadyTimeout), progressLogger(app, "openbao-post")); err != nil {
		return err
	}
	m := registry.New(kube, app.IO, r.ClusterName)
	return m.Push(ctx, r.Registry.Repository, r.Registry.Credentials)
}

func progressLogger(app *cli.App, name string) func(time.Duration) {
	last := time.Duration(0)
	return func(remaining time.Duration) {
		elapsed := defaultReadyTimeout - remaining
		if elapsed-last >= time.Minute {
			app.IO.Errf("  Still waiting for %s...\n", name)
			last = elapsed
		}
	}
}

func newStopCmd(app *cli.App) *cobra.Command {
	return &cobra.Command{
		Use:   "stop",
		Short: "Stop the cluster",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			r, err := resolve(app)
			if err != nil {
				return err
			}
			ctx, cancel := app.Context(clusterWaitTimeout)
			defer cancel()
			return providers.New(providerDeps(app, r)).Stop(ctx)
		},
	}
}

func newRestartCmd(app *cli.App) *cobra.Command {
	return &cobra.Command{
		Use:   "restart",
		Short: "Stop and restart the cluster",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			r, err := resolve(app)
			if err != nil {
				return err
			}
			ctx, cancel := app.Context(clusterWaitTimeout)
			defer cancel()
			provider := providers.New(providerDeps(app, r))
			if err := provider.Stop(ctx); err != nil {
				return err
			}
			return runStart(ctx, app, r, false)
		},
	}
}

func newConnectCmd(app *cli.App) *cobra.Command {
	var force bool
	c := &cobra.Command{
		Use:   "connect",
		Short: "Connect to the selected cluster",
		Long:  "Connect selects the active minikube profile or re-establishes the vcluster connection, depending on the selected cluster's provider.",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			r, err := resolve(app)
			if err != nil {
				return err
			}
			ctx, cancel := app.Context(clusterWaitTimeout)
			defer cancel()
			if r.Provider == config.ProviderMinikube {
				m := &providers.Minikube{Deps: providerDeps(app, r)}
				return m.Connect(ctx)
			}
			if r.Provider != config.ProviderVCluster {
				return cli.Exitf(cli.ExitUsage, "connect is only available for the minikube and vcluster providers (current: %s)", r.Provider)
			}
			// Without --force, leave an existing working connection untouched.
			if !force && kubernetes.ContextExists(ctx, runner(app), app.Flags.Kubeconfig, r.KubeContext) && kubeFor(app, r).Healthz(ctx) {
				app.IO.Errf("Already connected to vcluster %s (context %s); use --force to reconnect.\n", r.ClusterName, r.KubeContext)
				return nil
			}
			v := &providers.VCluster{Deps: providerDeps(app, r)}
			return v.Reconnect(ctx)
		},
	}
	c.Flags().BoolVar(&force, "force", false, "Force a vcluster reconnect even if the context already exists")
	return c
}

func newDeleteCmd(app *cli.App) *cobra.Command {
	c := &cobra.Command{
		Use:   "delete",
		Short: "Delete the cluster or a cluster object",
		Args:  cobra.NoArgs,
		RunE:  func(cmd *cobra.Command, args []string) error { return cmd.Help() },
	}
	c.AddCommand(
		newDeleteClusterCmd(app),
		newDeleteSourceCmd(app),
		newDeleteSecretCmd(app),
		newDeletePortForwardCmd(app),
	)
	return c
}

func newDeleteClusterCmd(app *cli.App) *cobra.Command {
	var force bool
	c := &cobra.Command{
		Use:   "cluster <name>",
		Short: "Delete the named cluster",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if app.Flags.Cluster != "" {
				return cli.Exitf(cli.ExitUsage, "-c/--cluster is not applicable to 'delete cluster'; pass the cluster name as an argument")
			}
			r, err := resolveNamed(app, args[0])
			if err != nil {
				return err
			}
			ctx, cancel := app.Context(clusterWaitTimeout)
			defer cancel()

			if force {
				app.Flags.Yes = true
			}
			ok, err := app.Confirm(fmt.Sprintf("This will delete the %s cluster '%s'. Are you sure?", r.Provider, r.ClusterName))
			if err != nil {
				return err
			}
			if !ok {
				app.IO.Errln("Aborted.")
				return nil
			}

			provider := providers.New(providerDeps(app, r))
			return provider.Delete(ctx)
		},
	}
	c.Flags().BoolVar(&force, "force", false, "Skip the confirmation prompt")
	return c
}

func newBootstrapCmd(app *cli.App) *cobra.Command {
	var force bool
	c := &cobra.Command{
		Use:   "bootstrap",
		Short: "Re-run the Flux bootstrap",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			r, err := resolve(app)
			if err != nil {
				return err
			}
			if force && !app.Flags.DryRun {
				ok, err := app.Confirm(fmt.Sprintf("--force overwrites the contents of %s/%s and pushes to %s before bootstrapping. Continue?", r.Git.Owner, r.Git.Repository, r.Git.Branch))
				if err != nil {
					return err
				}
				if !ok {
					app.IO.Errln("Aborted.")
					return nil
				}
			}
			if app.Flags.DryRun {
				app.IO.Errf("Would run flux bootstrap for %s/%s (branch %s, path %s)%s.\n", r.Git.Owner, r.Git.Repository, r.Git.Branch, r.ClonePath(), forceSuffix(force))
				return nil
			}
			ctx, cancel := app.Context(clusterWaitTimeout)
			defer cancel()
			kube := kubeFor(app, r)
			if !kube.Healthz(ctx) {
				return cli.Exitf(cli.ExitUnreachable, "cluster %s is not reachable; start it with 'leptostack start'", r.KubeContext)
			}
			gm := gitops.New(runner(app), app.IO, r, fluxFor(app, r))
			return gm.RunBootstrap(ctx, force)
		},
	}
	c.Flags().BoolVar(&force, "force", false, "Force a resync of the embedded cluster template")
	return c
}

func forceSuffix(force bool) string {
	if force {
		return " with a forced resync"
	}
	return ""
}

// --- status ---

type statusCluster struct {
	Name     string `json:"name"`
	Provider string `json:"provider"`
	State    string `json:"state"`
	Context  string `json:"context"`
}

type statusKustomizations struct {
	Total     int `json:"total"`
	Ready     int `json:"ready"`
	Suspended int `json:"suspended"`
}

type statusFlux struct {
	Version        string               `json:"version"`
	Kustomizations statusKustomizations `json:"kustomizations"`
}

type statusEndpoint struct {
	Domain    string `json:"domain"`
	GatewayIP string `json:"gatewayIP"`
}

type statusEnvironment struct {
	DNS          string `json:"dns"`
	CATrust      string `json:"caTrust"`
	PortForwards int    `json:"portForwards"`
}

type statusOutput struct {
	Cluster     statusCluster     `json:"cluster"`
	Flux        statusFlux        `json:"flux"`
	Endpoint    statusEndpoint    `json:"endpoint"`
	Environment statusEnvironment `json:"environment"`
}

func newStatusCmd(app *cli.App) *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show cluster, Flux and local environment status",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			r, err := resolve(app)
			if err != nil {
				return err
			}
			if err := app.ValidateOutput(cli.OutputJSON, cli.OutputYAML); err != nil {
				return err
			}
			ctx, cancel := app.Context(clusterWaitTimeout)
			defer cancel()
			return runStatus(ctx, app, r)
		},
	}
}

func runStatus(ctx context.Context, app *cli.App, r *config.Resolved) error {
	kube := kubeFor(app, r)
	provider := providers.New(providerDeps(app, r))

	out := statusOutput{
		Cluster:  statusCluster{Name: r.Name, Provider: r.Provider, Context: r.KubeContext},
		Endpoint: statusEndpoint{Domain: r.Domain},
	}
	out.Cluster.State = providerState(ctx, provider, kube)

	fluxVer := ""
	if v, err := fluxFor(app, r).Version(ctx); err == nil {
		fluxVer = v
	}
	out.Flux.Version = fluxVer

	ksList, _ := kustomizations.List(ctx, kube)
	out.Flux.Kustomizations.Total = len(ksList)
	for _, k := range ksList {
		if k.Suspended() {
			out.Flux.Kustomizations.Suspended++
			continue
		}
		if k.Ready() {
			out.Flux.Kustomizations.Ready++
		}
	}

	out.Endpoint.GatewayIP = gatewayIP(ctx, app, r, kube)
	if state, _ := portforward.ListStates(r.Name); state != nil {
		for _, s := range state {
			if s.Alive() {
				out.Environment.PortForwards++
			}
		}
	}

	dm := newDNSManager(app)
	if _, ok := dm.Get(r.Name); ok {
		out.Environment.DNS = "configured"
	} else {
		out.Environment.DNS = "absent"
	}
	store := ca.Default(config.Dir())
	if store.Exists() {
		if t := store.TrustStatus(ctx, runner(app), distro(), r.Domain); t != "" {
			out.Environment.CATrust = t
		} else {
			out.Environment.CATrust = "unknown"
		}
	} else {
		out.Environment.CATrust = "absent"
	}

	return output(app, out, func() {
		color := app.Colorizer()
		app.IO.Printf("=== LeptoStack Status ===\n\n")
		app.IO.Printf("Cluster:   %s (%s)\n", out.Cluster.Name, out.Cluster.Provider)
		app.IO.Printf("State:     %s\n", color.Status(out.Cluster.State))
		app.IO.Printf("Context:   %s\n", out.Cluster.Context)
		app.IO.Printf("Endpoint:  %s -> %s\n", out.Endpoint.Domain, out.Endpoint.GatewayIP)
		app.IO.Printf("DNS:       %s\n", color.Status(out.Environment.DNS))
		app.IO.Printf("CA trust:  %s\n", color.Status(out.Environment.CATrust))
		app.IO.Printf("Port-forwards: %d\n\n", out.Environment.PortForwards)

		app.IO.Printf("Flux version: %s\n\n", dashIfEmpty(out.Flux.Version))
		k := out.Flux.Kustomizations
		app.IO.Printf("Kustomizations: %d total, %d ready, %d suspended\n\n", k.Total, k.Ready, k.Suspended)

		if ksList != nil {
			t := ui.NewTable("NAME", "READY", "SUSPENDED", "MESSAGE")
			for _, item := range ksList {
				t.AddRow(item.Metadata.Name,
					color.Status(fmt.Sprintf("%t", item.Ready())),
					fmt.Sprintf("%t", item.Suspended()),
					truncate(item.Message(), 60))
			}
			t.Render(app.IO.Out)
		}
	})
}

func providerState(ctx context.Context, provider providers.Provider, kube *kubernetes.Client) string {
	if kube.Healthz(ctx) {
		return "Running"
	}
	return "Stopped"
}

func gatewayIP(ctx context.Context, app *cli.App, r *config.Resolved, kube *kubernetes.Client) string {
	if r.Provider == config.ProviderMinikube {
		m := &providers.Minikube{Deps: providerDeps(app, r)}
		return kubernetes.Subnet100(m.IP(ctx))
	}
	out, err := kube.Output(ctx, "-n", "ingress-"+r.ClusterName, "get", "service", "apisix-gateway",
		"-o", "jsonpath={.status.loadBalancer.ingress[0].ip}")
	if err == nil {
		return strings.TrimSpace(out)
	}
	return ""
}

func dashIfEmpty(s string) string {
	if strings.TrimSpace(s) == "" {
		return "-"
	}
	return s
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}
