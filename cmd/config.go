package cmd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/micronisus/leptostack-cli/internal/cli"
	"github.com/micronisus/leptostack-cli/internal/config"
	"github.com/micronisus/leptostack-cli/internal/execx"
)

type initOptions struct {
	provider             string
	clusterName          string
	leptostackName       string
	domain               string
	hostContext          string
	hostNamespace        string
	vclusterMetricsOff   bool
	vclusterOIDCOff      bool
	vclusterAPIServerSvc string
	cpus                 int
	memoryMB             int
	repoURL              string
	branch               string
	clusterPath          string
	gitServer            string
	gitPATStdin          bool
	gitPAT               string
	gitCAFile            string
	basePAT              bool
	baseURL              string
	baseBranch           string
	registryRepo         string
	registryStdin        bool
	registryCreds        string
	fromFile             string
	nonInteractive       bool
}

func newConfigCmd(app *cli.App) *cobra.Command {
	c := &cobra.Command{
		Use:   "config",
		Short: "Manage the leptostack configuration",
		Args:  cobra.NoArgs,
		RunE:  func(cmd *cobra.Command, args []string) error { return cmd.Help() },
	}
	c.AddCommand(
		newConfigInitCmd(app),
		newConfigViewCmd(app),
		newConfigSetCmd(app),
		newConfigUnsetCmd(app),
		newConfigUseContextCmd(app),
	)
	return c
}

func newConfigInitCmd(app *cli.App) *cobra.Command {
	opts := &initOptions{}
	c := &cobra.Command{
		Use:   "init",
		Short: "Add a new cluster to the configuration",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if opts.nonInteractive {
				app.Flags.NonInteractive = true
			}
			if app.Flags.Cluster != "" {
				return cli.Exitf(cli.ExitUsage, "-c/--cluster is not applicable to 'config init'; it always creates a new cluster")
			}
			return runConfigInit(cmd, app, opts)
		},
	}
	f := c.Flags()
	f.StringVar(&opts.provider, "provider", "", "Cluster provider: minikube or vcluster")
	f.StringVar(&opts.clusterName, "cluster-name", "", "Cluster name")
	f.StringVar(&opts.leptostackName, "leptostack-name", "", "LeptoStack name")
	f.StringVar(&opts.domain, "domain", "", "LeptoStack domain")
	f.StringVar(&opts.hostContext, "host-context", "", "vcluster host Kubernetes context")
	f.StringVar(&opts.hostNamespace, "host-namespace", "", "vcluster host namespace")
	f.BoolVar(&opts.vclusterMetricsOff, "disable-vcluster-metrics", false, "Disable the vcluster metrics-server integration")
	f.BoolVar(&opts.vclusterOIDCOff, "disable-vcluster-oidc", false, "Disable vcluster API server OIDC authentication")
	f.StringVar(&opts.vclusterAPIServerSvc, "vcluster-api-server-service-type", "", "vcluster API server Service type: loadbalancer or clusterip (default: loadbalancer)")
	f.IntVar(&opts.cpus, "cpus", 0, "minikube CPUs")
	f.IntVar(&opts.memoryMB, "memory", 0, "minikube memory in MB")
	f.StringVar(&opts.repoURL, "repo-url", "", "GitOps repository URL, or 'auto' to create one on start (default: auto)")
	f.StringVar(&opts.branch, "branch", "", "GitOps branch")
	f.StringVar(&opts.clusterPath, "cluster-path", "", "Path to the cluster within the repo")
	f.StringVar(&opts.gitServer, "git-server", "", "Git server: github, gitea or gitlab")
	f.BoolVar(&opts.gitPATStdin, "git-pat-stdin", false, "Read the Git PAT from stdin")
	f.StringVar(&opts.gitPAT, "git-pat", "", "Git PAT (prefer --git-pat-stdin or LEPTOSTACK_GIT_PAT)")
	f.StringVar(&opts.gitCAFile, "git-ca-file", "", "Path to a PEM CA bundle for the GitOps and leptostack-base git servers (enterprise CA)")
	f.BoolVar(&opts.basePAT, "base-pat-stdin", false, "Read the leptostack-base PAT from stdin")
	f.BoolVar(&opts.basePAT, "resources-pat-stdin", false, "Deprecated alias for --base-pat-stdin")
	_ = f.MarkHidden("resources-pat-stdin")
	f.StringVar(&opts.baseURL, "base-url", "", "leptostack-base repository URL")
	f.StringVar(&opts.baseBranch, "base-branch", "", "leptostack-base repository branch")
	f.StringVar(&opts.registryRepo, "registry-repository", "", "Container registry repository")
	f.BoolVar(&opts.registryStdin, "registry-credentials-stdin", false, "Read registry credentials (user:pass) from stdin")
	f.StringVar(&opts.registryCreds, "registry-credentials", "", "Registry credentials (user:pass)")
	f.StringVar(&opts.fromFile, "from-file", "", "Load the configuration from a YAML file")
	f.BoolVar(&opts.nonInteractive, "non-interactive", false, "Do not prompt; require all values via flags/env")
	return c
}

func runConfigInit(cmd *cobra.Command, app *cli.App, opts *initOptions) error {
	ctx, cancel := app.Context(defaultWaitTimeout)
	defer cancel()

	if opts.fromFile != "" {
		cfg, err := config.LoadFrom(opts.fromFile)
		if err != nil {
			return cli.MapConfigError(err)
		}
		if err := cfg.Save(); err != nil {
			return cli.MapConfigError(err)
		}
		app.IO.Errf("Configuration written to %s\n", config.DefaultPath())
		return nil
	}

	cfg, err := loadOrNew(app)
	if err != nil {
		return err
	}

	// config init always adds a new cluster; existing clusters are never
	// updated, so the provided name must be unique.
	cl := &config.Cluster{Provider: orString(opts.provider, config.DefaultProvider)}

	if err := validateProvider(cl.Provider); err != nil {
		return err
	}
	if err := ensureTool(ctx, app, "kubectl"); err != nil {
		return err
	}
	if err := ensureProviderTool(ctx, app, cl.Provider); err != nil {
		return err
	}

	interactive := app.Interactive() && !opts.nonInteractive

	// Provider choice.
	if opts.provider == "" && interactive {
		choice, err := app.Prompt("Select the cluster provider (1=minikube, 2=vcluster)", "1")
		if err != nil {
			return err
		}
		if choice == "2" || strings.EqualFold(choice, "vcluster") {
			cl.Provider = config.ProviderVCluster
		} else {
			cl.Provider = config.ProviderMinikube
		}
	}

	// Cluster name: it becomes the entry key and must not already exist.
	name, err := promptNewClusterName(cfg, app, opts.clusterName, interactive)
	if err != nil {
		return err
	}
	cl.ClusterName = name

	// LeptoStack name.
	cl.LeptostackName = orString(opts.leptostackName, orString(cl.LeptostackName, config.DefaultLeptostackName))
	if interactive && opts.leptostackName == "" {
		v, err := app.Prompt("Enter the LeptoStack name", cl.LeptostackName)
		if err != nil {
			return err
		}
		cl.LeptostackName = v
	}

	// Domain.
	cl.Domain = orString(opts.domain, orString(cl.Domain, config.DefaultDomain()))
	if interactive && opts.domain == "" {
		v, err := app.Prompt("Enter the LeptoStack domain", cl.Domain)
		if err != nil {
			return err
		}
		cl.Domain = v
	}
	if err := config.ValidateDomain(cl.Domain); err != nil {
		return cli.MapConfigError(err)
	}

	// Provider-specific settings.
	switch cl.Provider {
	case config.ProviderVCluster:
		if cl.VCluster == nil {
			cl.VCluster = &config.VCluster{}
		}
		cl.VCluster.HostContext = orString(opts.hostContext, cl.VCluster.HostContext)
		if cl.VCluster.HostContext == "" && interactive {
			def := currentKubeContext(ctx, app)
			v, err := app.Prompt("Enter the host Kubernetes context", def)
			if err != nil {
				return err
			}
			cl.VCluster.HostContext = v
		}
		if strings.TrimSpace(cl.VCluster.HostContext) == "" {
			return cli.Exitf(cli.ExitUsage, "--host-context is required for the vcluster provider")
		}
		cl.VCluster.HostNamespace = orString(opts.hostNamespace, orString(cl.VCluster.HostNamespace, config.DefaultHostNamespace()))
		if interactive && opts.hostNamespace == "" {
			v, err := app.Prompt("Enter the host namespace", cl.VCluster.HostNamespace)
			if err != nil {
				return err
			}
			cl.VCluster.HostNamespace = v
		}
		if opts.vclusterMetricsOff {
			cl.VCluster.Metrics = boolPtr(false)
		}
		if opts.vclusterOIDCOff {
			cl.VCluster.OIDC = boolPtr(false)
		}
		if opts.vclusterAPIServerSvc != "" {
			t, err := parseVClusterServiceType(opts.vclusterAPIServerSvc)
			if err != nil {
				return err
			}
			cl.VCluster.APIServerServiceType = t
		}
	case config.ProviderMinikube:
		if cl.Minikube == nil {
			cl.Minikube = &config.Minikube{}
		}
		if opts.cpus > 0 {
			cl.Minikube.CPUs = opts.cpus
		}
		if opts.memoryMB > 0 {
			cl.Minikube.MemoryMB = opts.memoryMB
		}
		if cl.Minikube.CPUs == 0 {
			cl.Minikube.CPUs = config.DefaultMinikubeCPUs
		}
		if cl.Minikube.MemoryMB == 0 {
			cl.Minikube.MemoryMB = config.DefaultMinikubeMemory
		}
	}

	// Git repository. The default is "auto": the repository and its Flux tokens
	// are created on `leptostack start`. Git is cluster-scoped.
	git := ensureClusterGit(cl)

	repoInput := opts.repoURL
	if repoInput == "" && interactive && git.Repository == "" {
		v, err := app.Prompt("Enter the LeptoStack development environment repository URL, or 'auto' to create one on start", config.AutoRepository)
		if err != nil {
			return err
		}
		repoInput = v
	}
	if repoInput == "" {
		repoInput = config.AutoRepository
	}

	autoMode := strings.EqualFold(strings.TrimSpace(repoInput), config.AutoRepository)
	if autoMode {
		git.Repository = config.AutoRepository
		git.Owner = ""
		git.PAT = ""
	} else {
		server, host, owner, repo, err := parseRepoURL(repoInput)
		if err != nil {
			return cli.Exitf(cli.ExitUsage, "%v", err)
		}
		git.Hostname, git.Owner, git.Repository = host, owner, repo
		if opts.gitServer != "" {
			git.Server = opts.gitServer
		} else {
			git.Server = server
		}
	}
	if opts.gitServer != "" {
		git.Server = opts.gitServer
	}
	if git.Server == "" {
		git.Server = config.DefaultGitServer
	}
	if err := validateGitServer(git.Server); err != nil {
		return err
	}
	git.Branch = orString(opts.branch, orString(git.Branch, config.DefaultGitBranch))
	git.ClusterPath = orString(opts.clusterPath, orString(git.ClusterPath, "clusters/"+cl.ClusterName))

	if opts.gitCAFile != "" {
		caFile, err := filepath.Abs(opts.gitCAFile)
		if err != nil {
			return cli.Exitf(cli.ExitUsage, "could not resolve --git-ca-file %q: %v", opts.gitCAFile, err)
		}
		if _, err := os.ReadFile(caFile); err != nil {
			return cli.Exitf(cli.ExitUsage, "could not read --git-ca-file %q: %v", caFile, err)
		}
		git.CAFile = caFile
	}

	// Secrets. When a concrete repository is configured (not auto), a PAT is
	// required for both the GitOps repository and the leptostack-base
	// repository. In auto mode the repository and its tokens are created on
	// `leptostack start`, so neither is required here.
	if !autoMode {
		pat, err := readPAT(app, interactive, "Enter the GitOps repository PAT", opts.gitPAT, opts.gitPATStdin, config.EnvGitPAT)
		if err != nil {
			return err
		}
		if pat == "" {
			return cli.Exitf(cli.ExitUsage, "a GitOps repository PAT is required for a configured repository; provide --git-pat, --git-pat-stdin or %s", config.EnvGitPAT)
		}
		git.PAT = pat
	}

	basePAT, err := readPAT(app, interactive && !autoMode, "Enter the leptostack-base repository PAT", "", opts.basePAT, config.EnvBasePAT, config.EnvResourcesPAT)
	if err != nil {
		return err
	}
	if !autoMode && basePAT == "" {
		return cli.Exitf(cli.ExitUsage, "a leptostack-base repository PAT is required for a configured repository; provide --base-pat-stdin or %s", config.EnvBasePAT)
	}
	if basePAT != "" {
		ensureClusterBase(cl).PAT = basePAT
	}

	if opts.registryCreds != "" {
		ensureClusterRegistry(cl).Credentials = opts.registryCreds
	} else if opts.registryStdin {
		creds, err := app.IO.ReadSecret()
		if err != nil {
			return err
		}
		ensureClusterRegistry(cl).Credentials = strings.TrimSpace(creds)
	}
	if r := cl.Registry; r != nil && r.Credentials != "" && !strings.Contains(r.Credentials, ":") {
		return cli.Exitf(cli.ExitUsage, "registry credentials must be in user:pass format")
	}

	if opts.baseURL != "" || opts.baseBranch != "" {
		b := ensureClusterBase(cl)
		if opts.baseURL != "" {
			b.URL = opts.baseURL
		}
		if opts.baseBranch != "" {
			b.Branch = opts.baseBranch
		}
	}
	if opts.registryRepo != "" {
		ensureClusterRegistry(cl).Repository = opts.registryRepo
	}

	// The entry key is the cluster name, matching `-c/--cluster` and the
	// derived context.
	cfg.Clusters[cl.ClusterName] = cl
	if cfg.CurrentContext == "" {
		cfg.CurrentContext = cl.ClusterName
	}
	if err := cfg.Save(); err != nil {
		return cli.MapConfigError(err)
	}

	if interactive {
		if cl.Provider == config.ProviderMinikube {
			if err := configureMinikubeResources(app, cl); err != nil {
				return err
			}
		}
	}

	app.IO.Errf("Configuration saved to %s\n\nTo start the development environment, run:\n  leptostack start -c %s\n", config.DefaultPath(), cl.ClusterName)
	return nil
}

// promptNewClusterName resolves the name of the cluster to create. The name must
// be valid and not already configured; interactively it keeps prompting until a
// free name is supplied.
func promptNewClusterName(cfg *config.Config, app *cli.App, provided string, interactive bool) (string, error) {
	name := strings.TrimSpace(provided)
	if name == "" {
		name = config.DefaultClusterName
		if interactive {
			v, err := app.Prompt("Enter the cluster name", name)
			if err != nil {
				return "", err
			}
			name = orString(v, name)
		}
	}
	for {
		switch err := clusterNameProblem(cfg, name); {
		case err == nil:
			return name, nil
		case !interactive:
			return "", err
		default:
			app.IO.Errf("%v\n", err)
		}
		v, err := app.Prompt("Enter the cluster name", "")
		if err != nil {
			return "", err
		}
		name = orString(v, config.DefaultClusterName)
	}
}

// clusterNameProblem reports why name cannot be used for a new cluster, or nil
// when it is valid and free.
func clusterNameProblem(cfg *config.Config, name string) error {
	if err := config.ValidateClusterName(name); err != nil {
		return cli.MapConfigError(err)
	}
	if cfg.Clusters[name] != nil {
		return cli.Exitf(cli.ExitConfig, "cluster %q already exists; choose a different name", name)
	}
	return nil
}

func newConfigViewCmd(app *cli.App) *cobra.Command {
	var showSecrets bool
	c := &cobra.Command{
		Use:   "view",
		Short: "Show the effective configuration (secrets redacted)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := app.Config()
			if err != nil {
				return err
			}
			out := cfg
			if !showSecrets {
				out = cfg.Redacted()
			}
			return app.EncodeYAML(out)
		},
	}
	c.Flags().BoolVar(&showSecrets, "show-secrets", false, "Reveal secret values")
	return c
}

func newConfigSetCmd(app *cli.App) *cobra.Command {
	var secret bool
	c := &cobra.Command{
		Use:   "set <key> <value>",
		Short: "Set a configuration value",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := app.Config()
			if err != nil {
				return err
			}
			if err := setConfigValueIn(cfg, app.Flags.Cluster, args[0], args[1], secret); err != nil {
				return err
			}
			if err := cfg.Save(); err != nil {
				return cli.MapConfigError(err)
			}
			return nil
		},
	}
	c.Flags().BoolVar(&secret, "secret", false, "Explicitly allow setting a secret-looking value")
	return c
}

func newConfigUnsetCmd(app *cli.App) *cobra.Command {
	c := &cobra.Command{
		Use:   "unset <key>",
		Short: "Clear a configuration value",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := app.Config()
			if err != nil {
				return err
			}
			if err := unsetConfigValueIn(cfg, app.Flags.Cluster, args[0]); err != nil {
				return err
			}
			if err := cfg.Save(); err != nil {
				return cli.MapConfigError(err)
			}
			return nil
		},
	}
	return c
}

func newConfigUseContextCmd(app *cli.App) *cobra.Command {
	c := &cobra.Command{
		Use:     "use-context <cluster>",
		Aliases: []string{"use-cluster"},
		Short:   "Switch the current cluster",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := app.Config()
			if err != nil {
				return err
			}
			if _, ok := cfg.Clusters[args[0]]; !ok {
				return cli.Exitf(cli.ExitConfig, "unknown cluster %q; known clusters: %s", args[0], strings.Join(clusterNames(cfg), ", "))
			}
			cfg.CurrentContext = args[0]
			if err := cfg.Save(); err != nil {
				return cli.MapConfigError(err)
			}
			app.IO.Errf("Switched to cluster %q.\n", args[0])
			return nil
		},
	}
	return c
}

func clusterNames(cfg *config.Config) []string {
	out := make([]string, 0, len(cfg.Clusters))
	for name := range cfg.Clusters {
		out = append(out, name)
	}
	return out
}

func currentKubeContext(ctx context.Context, app *cli.App) string {
	return parentCurrentContext(ctx, app)
}

func parentCurrentContext(ctx context.Context, app *cli.App) string {
	out, err := runner(app).OutputCombined(ctx, execx.Cmd{Name: "kubectl", Args: []string{"config", "current-context"}})
	if err != nil {
		return ""
	}
	return strings.TrimSpace(out)
}

func loadOrNew(app *cli.App) (*config.Config, error) {
	cfg, err := app.Config()
	if err != nil {
		if errMsg, ok := err.(*cli.ExitError); ok && errMsg.Code == cli.ExitConfig {
			return &config.Config{
				APIVersion: config.APIVersion,
				Kind:       config.Kind,
				Clusters:   map[string]*config.Cluster{},
			}, nil
		}
		return nil, err
	}
	if cfg.Clusters == nil {
		cfg.Clusters = map[string]*config.Cluster{}
	}
	return cfg, nil
}

func validateProvider(p string) error {
	switch p {
	case config.ProviderMinikube, config.ProviderVCluster:
		return nil
	}
	return cli.Exitf(cli.ExitUsage, "invalid provider %q (expected minikube or vcluster)", p)
}

func validateGitServer(s string) error {
	switch s {
	case config.ServerGitHub, config.ServerGitea, config.ServerGitLab:
		return nil
	}
	return cli.Exitf(cli.ExitUsage, "invalid git server %q (expected github, gitea or gitlab)", s)
}

func ensureTool(ctx context.Context, app *cli.App, name string) error {
	if _, ok := execx.LookPath(name); !ok {
		return cli.Exitf(cli.ExitPrereq, "%s is not installed; install it and retry", name)
	}
	return nil
}

func ensureProviderTool(ctx context.Context, app *cli.App, provider string) error {
	if provider == config.ProviderVCluster {
		return ensureTool(ctx, app, "vcluster")
	}
	return ensureTool(ctx, app, "minikube")
}

func orString(v, def string) string {
	if strings.TrimSpace(v) == "" {
		return def
	}
	return v
}

// readPAT returns a PAT sourced from stdin (when requested), the explicit flag,
// the environment, or an interactive prompt, in that order. It returns "" when
// no value is available and prompting is not possible.
func readPAT(app *cli.App, interactive bool, label, flagValue string, stdin bool, envKeys ...string) (string, error) {
	if stdin {
		v, err := app.IO.ReadSecret()
		if err != nil {
			return "", err
		}
		if v = strings.TrimSpace(v); v != "" {
			return v, nil
		}
	}
	if flagValue != "" {
		return flagValue, nil
	}
	if v := envFirst(envKeys...); v != "" {
		return v, nil
	}
	if interactive {
		return app.PromptSecret(label)
	}
	return "", nil
}

func envFirst(keys ...string) string {
	for _, k := range keys {
		if v := lookupEnv(k); v != "" {
			return v
		}
	}
	return ""
}

func lookupEnv(key string) string {
	if v, ok := os.LookupEnv(key); ok {
		return v
	}
	return ""
}

func parseRepoURL(raw string) (server, host, owner, repo string, err error) {
	s := strings.TrimSpace(raw)
	s = strings.TrimPrefix(s, "https://")
	s = strings.TrimPrefix(s, "http://")
	s = strings.TrimSuffix(s, ".git")
	parts := strings.Split(s, "/")
	if len(parts) < 3 || parts[0] == "" || parts[1] == "" || parts[2] == "" {
		return "", "", "", "", fmt.Errorf("could not parse repository URL %q; expected https://hostname/owner/repo", raw)
	}
	host, owner, repo = parts[0], parts[1], parts[2]
	switch {
	case strings.Contains(host, "github"):
		server = config.ServerGitHub
	case strings.Contains(host, "gitlab"):
		server = config.ServerGitLab
	default:
		server = config.ServerGitea
	}
	return server, host, owner, repo, nil
}

func configureMinikubeResources(app *cli.App, cl *config.Cluster) error {
	app.IO.Errf("Minikube will use cpus=%d memory=%dMB.\n", cl.Minikube.CPUs, cl.Minikube.MemoryMB)
	return nil
}
