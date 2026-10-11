// Package kustomizations lists Flux kustomizations and implements enable/disable
// (spec.suspend) plus readiness waiting and status mapping.
package kustomizations

import (
	"context"
	"fmt"
	"time"

	"github.com/micronisus/leptostack-cli/internal/kubernetes"
)

// Namespace is the Flux system namespace.
const Namespace = "flux-system"

// List returns all kustomizations in flux-system.
func List(ctx context.Context, kube *kubernetes.Client) ([]kubernetes.Kustomization, error) {
	var list kubernetes.KustomizationList
	if err := kube.GetJSON(ctx, &list, "kustomizations.kustomize.toolkit.fluxcd.io", "-n", Namespace); err != nil {
		return nil, err
	}
	return list.Items, nil
}

// Names returns the kustomization names in flux-system.
func Names(ctx context.Context, kube *kubernetes.Client) ([]string, error) {
	items, err := List(ctx, kube)
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(items))
	for _, k := range items {
		out = append(out, k.Metadata.Name)
	}
	return out, nil
}

// AllNames returns the names targeted by `all`. flux-system itself is excluded
// unless includeFluxSystem is set, to avoid suspending the whole GitOps
// controller by accident.
func AllNames(ctx context.Context, kube *kubernetes.Client, includeFluxSystem bool) ([]string, error) {
	items, err := List(ctx, kube)
	if err != nil {
		return nil, err
	}
	out := []string{}
	for _, k := range items {
		if k.Metadata.Name == "flux-system" && !includeFluxSystem {
			continue
		}
		out = append(out, k.Metadata.Name)
	}
	return out, nil
}

// Get reads a single kustomization.
func Get(ctx context.Context, kube *kubernetes.Client, name string) (kubernetes.Kustomization, error) {
	var k kubernetes.Kustomization
	if err := kube.GetJSON(ctx, &k, "kustomizations.kustomize.toolkit.fluxcd.io", name, "-n", Namespace); err != nil {
		return kubernetes.Kustomization{}, err
	}
	return k, nil
}

// IsReady reports whether the named kustomization's Ready condition is True.
func IsReady(ctx context.Context, kube *kubernetes.Client, name string) bool {
	k, err := Get(ctx, kube, name)
	return err == nil && k.Ready()
}

// Patch sets spec.suspend on a kustomization.
func Patch(ctx context.Context, kube *kubernetes.Client, name string, suspend bool) error {
	payload := fmt.Sprintf(`{"spec":{"suspend":%t}}`, suspend)
	return kube.Run(ctx, "patch", "kustomizations/"+name, "-n", Namespace, "--type", "merge", "-p", payload)
}

// Enable resumes reconciliation of the named kustomizations.
func Enable(ctx context.Context, kube *kubernetes.Client, names []string) ([]string, error) {
	return patchAll(ctx, kube, names, false)
}

// Disable suspends reconciliation of the named kustomizations.
func Disable(ctx context.Context, kube *kubernetes.Client, names []string) ([]string, error) {
	return patchAll(ctx, kube, names, true)
}

func patchAll(ctx context.Context, kube *kubernetes.Client, names []string, suspend bool) ([]string, error) {
	var changed []string
	for _, n := range names {
		if err := Patch(ctx, kube, n, suspend); err != nil {
			return changed, fmt.Errorf("patching kustomization %s: %w", n, err)
		}
		changed = append(changed, n)
	}
	return changed, nil
}

// WaitReady blocks until the named kustomization is ready or the timeout
// elapses, invoking progress with elapsed seconds every ~30s.
func WaitReady(ctx context.Context, kube *kubernetes.Client, name string, timeout time.Duration, progress func(elapsed time.Duration)) error {
	deadline := time.Now().Add(timeout)
	for {
		if IsReady(ctx, kube, name) {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("timed out after %s waiting for %s to become ready", timeout, name)
		}
		if progress != nil {
			progress(time.Until(deadline))
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(10 * time.Second):
		}
	}
}
