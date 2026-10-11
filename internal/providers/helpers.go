package providers

import (
	"context"
	"strings"

	"github.com/micronisus/leptostack-cli/internal/execx"
	"github.com/micronisus/leptostack-cli/internal/kubernetes"
)

func contextExists(ctx context.Context, r *execx.Runner, kubeconfig, name string) bool {
	return kubernetes.ContextExists(ctx, r, kubeconfig, name)
}

func useContext(ctx context.Context, r *execx.Runner, kubeconfig, name string) error {
	return kubernetes.UseContext(ctx, r, kubeconfig, name)
}

func deleteContext(ctx context.Context, r *execx.Runner, kubeconfig, name string) error {
	return kubernetes.DeleteContext(ctx, r, kubeconfig, name)
}

func currentContext(ctx context.Context, r *execx.Runner, kubeconfig string) string {
	return kubernetes.CurrentContext(ctx, r, kubeconfig)
}

func trim(s string) string { return strings.TrimSpace(s) }

func contains(s, sub string) bool { return strings.Contains(s, sub) }
