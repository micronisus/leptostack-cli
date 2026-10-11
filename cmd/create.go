package cmd

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/micronisus/leptostack-cli/internal/cli"
	"github.com/micronisus/leptostack-cli/internal/gitops"
	"github.com/micronisus/leptostack-cli/internal/kubernetes"
	"github.com/micronisus/leptostack-cli/internal/secrets"
	"github.com/micronisus/leptostack-cli/internal/sources"
)

func newCreateCmd(app *cli.App) *cobra.Command {
	c := &cobra.Command{
		Use:   "create",
		Short: "Create a source or secret object",
		Args:  cobra.NoArgs,
		RunE:  func(cmd *cobra.Command, args []string) error { return cmd.Help() },
	}
	c.AddCommand(newCreateSourceCmd(app), newCreateSecretCmd(app))
	return c
}

type sourceFlags struct {
	namespace       string
	url             string
	branch          string
	tag             string
	interval        string
	secretRef       string
	bucketName      string
	region          string
	provider        string
	passCredentials bool
	insecure        bool
	toGit           bool
	direct          bool
}

func newCreateSourceCmd(app *cli.App) *cobra.Command {
	c := &cobra.Command{
		Use:   "source",
		Short: "Create a Flux source",
		Args:  cobra.NoArgs,
		RunE:  func(cmd *cobra.Command, args []string) error { return cmd.Help() },
	}
	c.AddCommand(
		newCreateSourceTypeCmd(app, sources.TypeGit),
		newCreateSourceTypeCmd(app, sources.TypeHelm),
		newCreateSourceTypeCmd(app, sources.TypeBucket),
		newCreateSourceTypeCmd(app, sources.TypeOCI),
	)
	return c
}

func newCreateSourceTypeCmd(app *cli.App, typ string) *cobra.Command {
	f := &sourceFlags{}
	c := &cobra.Command{
		Use:   typ + " <name>",
		Short: "Create a " + typ + " source",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runCreateSource(cmd, app, typ, args[0], f)
		},
	}
	fl := c.Flags()
	fl.StringVar(&f.url, "url", "", "Source URL (required)")
	fl.StringVar(&f.interval, "interval", "5m", "Reconciliation interval")
	fl.StringVar(&f.secretRef, "secret-ref", "", "Referenced secret name")
	fl.StringVarP(&f.namespace, "namespace", "n", "", "Namespace (default: flux-system)")
	fl.BoolVar(&f.toGit, "git", false, "Write the manifest into the GitOps repository")
	fl.BoolVar(&f.direct, "direct", false, "Apply the manifest to the cluster now (default)")
	switch typ {
	case sources.TypeGit:
		fl.StringVar(&f.branch, "branch", "", "Git branch")
		fl.StringVar(&f.tag, "tag", "", "Git tag")
	case sources.TypeBucket:
		fl.StringVar(&f.bucketName, "bucket-name", "", "Bucket name (required)")
		fl.StringVar(&f.region, "region", "", "Bucket region")
		fl.StringVar(&f.provider, "provider", "", "Bucket provider (e.g. generic, aws)")
	case sources.TypeHelm:
		fl.BoolVar(&f.passCredentials, "pass-credentials", false, "Pass credentials to the Helm repository")
	case sources.TypeOCI:
		fl.BoolVar(&f.insecure, "insecure-allow-http", false, "Allow insecure HTTP OCI registry")
	}
	return c
}

func runCreateSource(cmd *cobra.Command, app *cli.App, typ, name string, f *sourceFlags) error {
	r, err := resolve(app)
	if err != nil {
		return err
	}
	if f.toGit && f.direct {
		return cli.Exitf(cli.ExitUsage, "--git and --direct are mutually exclusive")
	}
	if app.Flags.DryRun {
		// dry-run needs no cluster.
	}
	req := sources.Request{
		Type: typ, Name: name, Namespace: f.namespace, URL: f.url,
		Branch: f.branch, Tag: f.tag, Interval: f.interval, SecretRef: f.secretRef,
		BucketName: f.bucketName, Region: f.region, Provider: f.provider,
		PassCredentials: f.passCredentials, Insecure: f.insecure,
	}
	if err := req.Validate(); err != nil {
		return cli.Exitf(cli.ExitUsage, "%v", err)
	}
	manifest, err := req.Manifest()
	if err != nil {
		return err
	}

	if app.Flags.DryRun {
		app.IO.Printf("%s", manifest)
		return nil
	}

	ctx, cancel := app.Context(clusterWaitTimeout)
	defer cancel()
	kube := kubeFor(app, r)

	if req.SecretRef != "" {
		if err := validateSecretRef(ctx, kube, req); err != nil {
			return err
		}
	}

	if f.toGit {
		gm := gitops.New(runner(app), app.IO, r, fluxFor(app, r))
		relPath := filepath.ToSlash(filepath.Join(r.ClonePath(), "sources", typ, name+".yaml"))
		if err := gm.CommitFile(ctx, relPath, manifest, fmt.Sprintf("Add %s source %s", typ, name)); err != nil {
			return err
		}
		app.IO.Errf("Source %s written to %s and pushed.\n", name, relPath)
		return nil
	}

	if err := kube.ApplyJSON(ctx, manifest); err != nil {
		return err
	}
	app.IO.Errf("Source %s (%s) applied to the cluster in namespace %s.\n", name, typ, namespaceOr(req.Namespace))
	return nil
}

func validateSecretRef(ctx context.Context, kube *kubernetes.Client, req sources.Request) error {
	ns := namespaceOr(req.Namespace)
	var secret kubernetes.Secret
	err := kube.GetJSON(ctx, &secret, "secret", req.SecretRef, "-n", ns)
	found := err == nil
	var keys []string
	if found {
		for k := range secret.Data {
			keys = append(keys, k)
		}
		for k := range secret.StringData {
			keys = append(keys, k)
		}
	}
	if err := req.ValidateSecretRef(keys, found); err != nil {
		return cli.Exitf(cli.ExitUsage, "%v", err)
	}
	return nil
}

func namespaceOr(ns string) string {
	if strings.TrimSpace(ns) == "" {
		return "flux-system"
	}
	return ns
}

// --- secrets ---

type secretFlags struct {
	namespace     string
	username      string
	password      string
	passwordStdin bool
	token         string
	registry      string
	cert          string
	key           string
	literals      []string
	files         []string
}

func newCreateSecretCmd(app *cli.App) *cobra.Command {
	c := &cobra.Command{
		Use:   "secret",
		Short: "Create a Kubernetes secret for cluster use",
		Args:  cobra.NoArgs,
		RunE:  func(cmd *cobra.Command, args []string) error { return cmd.Help() },
	}
	c.AddCommand(
		newCreateSecretKindCmd(app, secrets.KindGit),
		newCreateSecretKindCmd(app, secrets.KindDockerRegistry),
		newCreateSecretKindCmd(app, secrets.KindTLS),
		newCreateSecretKindCmd(app, secrets.KindOpaque),
	)
	return c
}

func newCreateSecretKindCmd(app *cli.App, kind string) *cobra.Command {
	f := &secretFlags{}
	c := &cobra.Command{
		Use:   kind + " <name>",
		Short: "Create a " + kind + " secret",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runCreateSecret(cmd, app, kind, args[0], f)
		},
	}
	fl := c.Flags()
	fl.StringVarP(&f.namespace, "namespace", "n", "", "Namespace (default: flux-system)")
	switch kind {
	case secrets.KindGit:
		fl.StringVar(&f.username, "username", "", "Git username")
		fl.StringVar(&f.password, "password", "", "Git password")
		fl.BoolVar(&f.passwordStdin, "password-stdin", false, "Read the password from stdin")
		fl.StringVar(&f.token, "token", "", "Git token (used as the password)")
	case secrets.KindDockerRegistry:
		fl.StringVar(&f.registry, "registry", "", "Registry host (required)")
		fl.StringVar(&f.username, "username", "", "Registry username")
		fl.StringVar(&f.password, "password", "", "Registry password")
		fl.BoolVar(&f.passwordStdin, "password-stdin", false, "Read the password from stdin")
	case secrets.KindTLS:
		fl.StringVar(&f.cert, "cert", "", "Certificate file (required)")
		fl.StringVar(&f.key, "key", "", "Key file (required)")
	case secrets.KindOpaque:
		fl.StringArrayVar(&f.literals, "from-literal", nil, "key=value literal (repeatable)")
		fl.StringArrayVar(&f.files, "from-file", nil, "key=path file (repeatable)")
	}
	return c
}

func runCreateSecret(cmd *cobra.Command, app *cli.App, kind, name string, f *secretFlags) error {
	if f.passwordStdin {
		pw, err := app.IO.ReadSecret()
		if err != nil {
			return err
		}
		f.password = strings.TrimSpace(pw)
	}

	req := secrets.Request{
		Kind: kind, Name: name, Namespace: f.namespace,
		Username: f.username, Password: f.password, Token: f.token,
		Registry: f.registry, CertFile: f.cert, KeyFile: f.key,
		Literals: map[string]string{}, Files: map[string]string{},
	}
	for _, lit := range f.literals {
		kv := strings.SplitN(lit, "=", 2)
		if len(kv) != 2 {
			return cli.Exitf(cli.ExitUsage, "--from-literal must be key=value (got %q)", lit)
		}
		req.Literals[kv[0]] = kv[1]
	}
	for _, file := range f.files {
		kv := strings.SplitN(file, "=", 2)
		if len(kv) != 2 {
			return cli.Exitf(cli.ExitUsage, "--from-file must be key=path (got %q)", file)
		}
		req.Files[kv[0]] = kv[1]
	}

	manifest, err := req.Manifest()
	if err != nil {
		return cli.Exitf(cli.ExitUsage, "%v", err)
	}
	if app.Flags.DryRun {
		app.IO.Printf("%s", manifest)
		return nil
	}

	r, err := resolve(app)
	if err != nil {
		return err
	}
	ctx, cancel := app.Context(clusterWaitTimeout)
	defer cancel()
	kube := kubeFor(app, r)
	if err := kube.ApplyJSON(ctx, manifest); err != nil {
		return err
	}
	app.IO.Errf("Secret %s (%s) applied to namespace %s.\n", name, kind, namespaceOr(f.namespace))
	return nil
}
