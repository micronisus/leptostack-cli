// Package providers implements the cluster provider branching (minikube and
// vcluster) behind a small interface so provider behavior is unit-testable with
// fake binaries.
package providers

import (
	"context"

	"github.com/micronisus/leptostack-cli/internal/ca"
	"github.com/micronisus/leptostack-cli/internal/cli"
	"github.com/micronisus/leptostack-cli/internal/config"
	"github.com/micronisus/leptostack-cli/internal/execx"
	"github.com/micronisus/leptostack-cli/internal/flux"
	"github.com/micronisus/leptostack-cli/internal/kubernetes"
)

// Deps carries everything a provider needs.
type Deps struct {
	R        *execx.Runner
	IO       *cli.IOStreams
	Resolved *config.Resolved
	Kube     *kubernetes.Client
	Flux     *flux.Client
	CA       ca.Store
	Distro   string
	// Kubeconfig is the --kubeconfig override, if any.
	Kubeconfig string
}

// Provider is the lifecycle interface implemented by both providers.
type Provider interface {
	Start(ctx context.Context) error
	Stop(ctx context.Context) error
	Delete(ctx context.Context) error
	Status(ctx context.Context) error
}

// New returns the provider matching the resolved configuration.
func New(d Deps) Provider {
	if d.Resolved.Provider == config.ProviderVCluster {
		return &VCluster{Deps: d}
	}
	return &Minikube{Deps: d}
}
