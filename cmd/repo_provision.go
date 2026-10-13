package cmd

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/micronisus/leptostack-cli/internal/cli"
	"github.com/micronisus/leptostack-cli/internal/config"
	"github.com/micronisus/leptostack-cli/internal/forge"
)

// forgeTokenEnv supplies a management token for API-based token minting
// (required for Forgejo/Gitea, where the fj CLI cannot create tokens).
const forgeTokenEnv = "LEPTOSTACK_FORGE_TOKEN"

// provisionAutoRepository materializes the "auto" GitOps repository: it verifies
// the forge CLI, creates a uniquely named repository under the personal account,
// mints the push/pull and read-only Flux tokens, and updates both the config and
// the resolved view so the subsequent template sync and bootstrap use it.
func provisionAutoRepository(ctx context.Context, app *cli.App, r *config.Resolved) error {
	if !r.AutoRepo {
		return nil
	}

	server, err := forge.ParseServer(orString(r.Git.Server, config.DefaultGitServer))
	if err != nil {
		return cli.Exitf(cli.ExitUsage, "%v", err)
	}
	hostname := r.Git.Hostname

	client, err := forge.New(server, runner(app), forge.Options{
		Hostname:   hostname,
		ForgeToken: os.Getenv(forgeTokenEnv),
		TokenDays:  365,
		Private:    true,
	})
	if err != nil {
		return cli.Exitf(cli.ExitUsage, "%v", err)
	}
	if err := client.EnsureCLI(); err != nil {
		return err
	}

	owner := r.Git.Owner
	if owner == "" {
		owner, err = client.Login(ctx)
		if err != nil {
			return err
		}
	}

	repoName, err := forge.UniqueRepoName(ctx, client, owner, r.ClusterName+"-gitops")
	if err != nil {
		return err
	}

	baseRepo, err := autoReadTokenRepo(r)
	if err != nil {
		return err
	}

	if app.Flags.DryRun {
		app.IO.Errf("Would create %s repository %s/%s (private) and mint a Flux token and a read-only token for %s.\n",
			server, owner, repoName, baseRepo.FullName())
		return nil
	}

	app.IO.Errf("GitOps repository not configured; creating %s/%s via %s...\n", owner, repoName, client.CLI())
	repo, err := client.CreateRepo(ctx, owner, repoName, true)
	if err != nil {
		return err
	}

	stamp := time.Now().UTC().Unix()
	repoCred, err := client.CreateRepoToken(ctx, repo, fmt.Sprintf("leptostack-%s-flux-%d", repoName, stamp))
	if err != nil {
		return err
	}
	readCred, err := client.CreateReadToken(ctx, owner, []forge.Repo{baseRepo}, fmt.Sprintf("leptostack-%s-read-%d", r.ClusterName, stamp))
	if err != nil {
		return err
	}

	if err := persistRepository(app, r, server, hostname, owner, repoName, repoCred.Token, readCred.Token); err != nil {
		return err
	}

	// Update the in-memory view so the template sync and bootstrap see the repo.
	r.Git.Server = string(server)
	r.Git.Hostname = hostname
	r.Git.Owner = owner
	r.Git.Repository = repoName
	r.Git.PAT = repoCred.Token
	r.Base.PAT = readCred.Token
	r.AutoRepo = false

	app.IO.Errf("Created repository %s and updated %s\n", repo.FullName(), config.DefaultPath())
	if repoCred.Note != "" {
		app.IO.Errf("  note: %s\n", repoCred.Note)
	}
	if readCred.Note != "" {
		app.IO.Errf("  note: %s\n", readCred.Note)
	}
	return nil
}

func autoReadTokenRepo(r *config.Resolved) (forge.Repo, error) {
	repo, err := forge.RepoFromURL(r.Base.URL)
	if err != nil {
		return forge.Repo{}, cli.Exitf(cli.ExitConfig, "could not derive the leptostack-base repository from %q", r.Base.URL)
	}
	return repo, nil
}

// persistRepository records the materialized repository and tokens in the
// per-cluster config so later runs (and other tools) use the real values.
func persistRepository(app *cli.App, r *config.Resolved, server forge.Server, hostname, owner, repoName, pushToken, readToken string) error {
	cfg, err := app.Config()
	if err != nil {
		return err
	}
	cl := cfg.Clusters[r.Name]
	if cl == nil {
		cl = &config.Cluster{ClusterName: r.ClusterName, Provider: r.Provider}
		cfg.Clusters[r.Name] = cl
	}
	cl.Git = &config.Git{
		Server:      string(server),
		Hostname:    hostname,
		Owner:       owner,
		Repository:  repoName,
		Branch:      orString(r.Git.Branch, config.DefaultGitBranch),
		ClusterPath: orString(r.Git.ClusterPath, "clusters/"+r.ClusterName),
		PAT:         pushToken,
	}
	if cl.Base == nil {
		cl.Base = &config.Base{}
	}
	cl.Base.PAT = readToken
	if cfg.CurrentContext == "" {
		cfg.CurrentContext = r.Name
	}
	return cli.MapConfigError(cfg.Save())
}
