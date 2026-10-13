package providers

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/micronisus/leptostack-cli/internal/config"
	"github.com/micronisus/leptostack-cli/internal/execx"
	"github.com/micronisus/leptostack-cli/internal/runtime"
)

// VCluster implements Provider for the vcluster provider.
type VCluster struct {
	Deps
}

// requireHost validates the configured host context and namespace.
func (v *VCluster) requireHost(ctx context.Context) error {
	r := v.Resolved
	if r.VCluster.HostContext == "" || r.VCluster.HostNamespace == "" {
		return fmt.Errorf("hostContext and hostNamespace must be set for vcluster operations; re-run 'leptostack config init --provider vcluster'")
	}
	if !contextExists(ctx, v.R, v.Kubeconfig, r.VCluster.HostContext) {
		return fmt.Errorf("host Kubernetes context %q was not found; check 'kubectl config get-contexts'", r.VCluster.HostContext)
	}
	return nil
}

// Start creates or resumes the vcluster and connects to it.
func (v *VCluster) Start(ctx context.Context) error {
	if err := v.requireHost(ctx); err != nil {
		return err
	}
	oidc := v.Resolved.VCluster.OIDCEnabled()
	if err := v.ensureHostNamespace(ctx); err != nil {
		return err
	}
	if oidc {
		if err := v.publishOIDCCASecret(ctx); err != nil {
			return err
		}
	}

	valuesFile, cleanup, err := v.writeValues()
	if err != nil {
		return err
	}
	defer cleanup()

	if v.Exists(ctx) {
		v.IO.Errf("Resuming vcluster %s...\n", v.Resolved.ClusterName)
		// Resuming an already-running vcluster reports an error; ignore it so
		// re-running start stays idempotent.
		_ = v.R.RunDiscard(ctx, v.vclusterCmd("resume", v.Resolved.ClusterName))
		if contextExists(ctx, v.R, v.Kubeconfig, v.Resolved.KubeContext) {
			if err := useContext(ctx, v.R, v.Kubeconfig, v.Resolved.KubeContext); err != nil {
				return err
			}
			_ = runtime.WriteVClusterContext(v.Resolved.Name, v.Resolved.KubeContext)
			v.IO.Errf("  vcluster context: %s\n", v.Resolved.KubeContext)
		} else {
			if err := v.Connect(ctx); err != nil {
				return err
			}
		}
	} else {
		v.IO.Errf("Creating vcluster %s in namespace %s on context %s...\n", v.Resolved.ClusterName, v.Resolved.VCluster.HostNamespace, v.Resolved.VCluster.HostContext)
		// Drop a stale context with the same generated name so create does not prompt.
		_ = deleteContext(ctx, v.R, v.Kubeconfig, v.Resolved.KubeContext)
		// The host namespace is created above (ensureHostNamespace). Pass
		// --create-namespace=false so Helm does not attempt a cluster-scoped
		// namespace create/get, which a namespace-scoped ServiceAccount
		// (the Nexus Che Job) is not allowed to perform.
		cmd := v.vclusterCmd(v.createArgs(valuesFile)...)
		if err := v.R.Run(ctx, cmd); err != nil {
			return fmt.Errorf("failed to create vcluster %s: %w", v.Resolved.ClusterName, err)
		}
		if v.apiServerIsClusterIP() {
			// create ran with --connect=false, so connect explicitly.
			if err := v.Connect(ctx); err != nil {
				return err
			}
		} else if err := v.adoptContext(ctx); err != nil {
			return err
		}
	}

	if oidc {
		if err := v.applyOIDCRBAC(ctx); err != nil {
			return err
		}
	}
	v.IO.Errln()
	return nil
}

// createArgs are the `vcluster create` arguments. `--create-namespace=false`
// keeps the operation namespace-scoped: the host namespace already exists
// (created by ensureHostNamespace), and a namespace-scoped ServiceAccount (the
// Nexus Che Job) cannot create or read Namespaces at cluster scope.
func (v *VCluster) createArgs(valuesFile string) []string {
	args := []string{"create", v.Resolved.ClusterName, "--values", valuesFile, "--create-namespace=false"}
	if v.apiServerIsClusterIP() {
		// A ClusterIP API server is not reachable from outside, so `vcluster
		// create` would hold a foreground port-forward. Connect explicitly with
		// the in-cluster Service address instead (see connectArgs).
		args = append(args, "--connect=false")
	}
	return args
}

// apiServerIsClusterIP reports whether the vcluster API server Service uses the
// ClusterIP type (i.e. it has no externally reachable endpoint).
func (v *VCluster) apiServerIsClusterIP() bool {
	return v.Resolved.VCluster.APIServerServiceTypeOrDefault() == config.VClusterServiceTypeClusterIP
}

// connectArgs are the `vcluster connect` arguments. For a ClusterIP API server
// the in-cluster Service DNS (`<name>.<namespace>`, which the API server
// certificate includes as a SAN) is passed via --server so connect does not
// fall back to a foreground port-forward.
func (v *VCluster) connectArgs() []string {
	args := []string{"connect", v.Resolved.ClusterName}
	if v.apiServerIsClusterIP() {
		server := fmt.Sprintf("https://%s.%s:443", v.Resolved.ClusterName, v.Resolved.VCluster.HostNamespace)
		args = append(args, "--server", server)
	}
	return args
}

// Stop pauses the vcluster.
func (v *VCluster) Stop(ctx context.Context) error {
	if err := v.requireHost(ctx); err != nil {
		return err
	}
	v.IO.Errf("Pausing vcluster %s...\n", v.Resolved.ClusterName)
	if err := v.R.Run(ctx, v.vclusterCmd("pause", v.Resolved.ClusterName)); err != nil {
		return err
	}
	v.IO.Errln("vcluster paused.")
	return nil
}

// Delete deletes the vcluster and its kubectl context.
func (v *VCluster) Delete(ctx context.Context) error {
	if err := v.requireHost(ctx); err != nil {
		return err
	}
	if v.Exists(ctx) {
		v.IO.Errf("Deleting vcluster %s...\n", v.Resolved.ClusterName)
		if err := v.R.Run(ctx, v.vclusterCmd("delete", v.Resolved.ClusterName)); err != nil {
			return err
		}
	} else {
		v.IO.Errf("vcluster %s is already deleted; skipping.\n", v.Resolved.ClusterName)
	}
	_ = deleteContext(ctx, v.R, v.Kubeconfig, v.Resolved.KubeContext)
	return nil
}

// Status prints the vcluster list for the host.
func (v *VCluster) Status(ctx context.Context) error {
	if err := v.requireHost(ctx); err != nil {
		return err
	}
	return v.R.Run(ctx, execx.Cmd{Name: "vcluster", Args: []string{"list",
		"--context", v.Resolved.VCluster.HostContext,
		"--namespace", v.Resolved.VCluster.HostNamespace}, Env: v.vclusterEnv()})
}

// Exists reports whether the vcluster statefulset is present on the host.
func (v *VCluster) Exists(ctx context.Context) bool {
	out, err := v.R.OutputCombined(ctx, execx.Cmd{Name: "kubectl", Args: v.hostKubeArgs(
		"-n", v.Resolved.VCluster.HostNamespace,
		"get", "statefulset", "-l", fmt.Sprintf("app=vcluster,release=%s", v.Resolved.ClusterName), "-o", "name")})
	if err != nil {
		return false
	}
	return strings.TrimSpace(out) != ""
}

// Reconnect recreates the vcluster connection and adopts the context.
func (v *VCluster) Reconnect(ctx context.Context) error {
	if err := v.requireHost(ctx); err != nil {
		return err
	}
	v.IO.Errf("=== Reconnecting to vcluster %s ===\n\n", v.Resolved.ClusterName)
	// Return to the host context before reconnecting.
	if err := useContext(ctx, v.R, v.Kubeconfig, v.Resolved.VCluster.HostContext); err != nil {
		return err
	}
	v.IO.Errf("Connecting to vcluster %s in namespace %s on context %s...\n", v.Resolved.ClusterName, v.Resolved.VCluster.HostNamespace, v.Resolved.VCluster.HostContext)
	if err := v.Connect(ctx); err != nil {
		return err
	}
	v.IO.Errf("\nReconnected to vcluster %s.\n", v.Resolved.ClusterName)
	return nil
}

// Connect runs vcluster connect and adopts the generated context.
func (v *VCluster) Connect(ctx context.Context) error {
	if err := v.R.Run(ctx, v.vclusterCmd(v.connectArgs()...)); err != nil {
		return err
	}
	return v.adoptContext(ctx)
}

func (v *VCluster) adoptContext(ctx context.Context) error {
	connected := currentContext(ctx, v.R, v.Kubeconfig)
	kc := v.Resolved.KubeContext
	if connected != "" && connected != v.Resolved.VCluster.HostContext {
		kc = connected
	}
	if contextExists(ctx, v.R, v.Kubeconfig, kc) {
		if err := useContext(ctx, v.R, v.Kubeconfig, kc); err != nil {
			return err
		}
		_ = runtime.WriteVClusterContext(v.Resolved.Name, kc)
		v.IO.Errf("  vcluster context: %s\n", kc)
		return nil
	}
	v.IO.Errf("Warning: Could not find a vcluster context. Run 'leptostack connect' to refresh the connection.\n")
	return nil
}

func (v *VCluster) vclusterCmd(args ...string) execx.Cmd {
	full := append([]string{}, args...)
	full = append(full,
		"--namespace", v.Resolved.VCluster.HostNamespace,
		"--context", v.Resolved.VCluster.HostContext)
	return execx.Cmd{Name: "vcluster", Args: full, Env: v.vclusterEnv()}
}

// vclusterEnv carries the KUBECONFIG override for vcluster CLI invocations.
// vcluster v0.37.x has no --kubeconfig flag, so the kubeconfig is propagated
// through the environment instead.
func (v *VCluster) vclusterEnv() map[string]string {
	if v.Kubeconfig == "" {
		return nil
	}
	return map[string]string{"KUBECONFIG": v.Kubeconfig}
}

// ensureHostNamespace creates the vcluster host namespace when it is absent.
// The host ServiceAccount is namespace-scoped, so the namespace must exist
// before `vcluster create --create-namespace=false`.
func (v *VCluster) ensureHostNamespace(ctx context.Context) error {
	ns := v.Resolved.VCluster.HostNamespace
	if err := v.R.RunDiscard(ctx, execx.Cmd{Name: "kubectl", Args: v.hostKubeArgs("get", "namespace", ns)}); err != nil {
		v.IO.Errf("Creating host namespace %s...\n", ns)
		if err := v.R.Run(ctx, execx.Cmd{Name: "kubectl", Args: v.hostKubeArgs("create", "namespace", ns)}); err != nil {
			return err
		}
	}
	return nil
}

// publishOIDCCASecret publishes the internal CA to the host namespace for the
// vcluster API server OIDC configuration.
func (v *VCluster) publishOIDCCASecret(ctx context.Context) error {
	if err := v.CA.Ensure(); err != nil {
		return err
	}
	ns := v.Resolved.VCluster.HostNamespace
	secretName := v.Resolved.ClusterName + "-oidc-ca"
	v.IO.Errf("Publishing the internal CA to host secret %s/%s for the vCluster API server OIDC configuration...\n", ns, secretName)
	dry, err := v.R.Output(ctx, execx.Cmd{Name: "kubectl", Args: v.hostKubeArgs(
		"-n", ns, "create", "secret", "generic", secretName,
		"--from-file=ca.crt="+v.CA.CertFile, "--dry-run=client", "-o", "yaml")})
	if err != nil {
		return err
	}
	apply := execx.Cmd{Name: "kubectl", Args: v.hostKubeArgs("-n", ns, "apply", "-f", "-")}
	apply.Stdin = strings.NewReader(dry)
	return v.R.RunDiscard(ctx, apply)
}

// applyOIDCRBAC grants cluster-admin to the cluster developers group.
func (v *VCluster) applyOIDCRBAC(ctx context.Context) error {
	group := v.Resolved.ClusterName + "-developers"
	manifest := fmt.Sprintf(`apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: oidc-%s-developers-cluster-admin
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: cluster-admin
subjects:
  - kind: Group
    apiGroup: rbac.authorization.k8s.io
    name: %s
`, v.Resolved.ClusterName, group)

	v.IO.Errf("Applying OIDC cluster-admin ClusterRoleBinding for group %s...\n", group)
	var lastErr error
	for i := 0; i < 10; i++ {
		cmd := execx.Cmd{Name: "kubectl", Args: v.kubeArgs("apply", "-f", "-")}
		cmd.Stdin = strings.NewReader(manifest)
		if err := v.R.RunDiscard(ctx, cmd); err == nil {
			v.IO.Errln("  ClusterRoleBinding applied.")
			return nil
		} else {
			lastErr = err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(3 * time.Second):
		}
	}
	return fmt.Errorf("failed to apply the OIDC ClusterRoleBinding: %w", lastErr)
}

func (v *VCluster) kubeArgs(args ...string) []string {
	full := []string{}
	if v.Kubeconfig != "" {
		full = append(full, "--kubeconfig", v.Kubeconfig)
	}
	if v.Resolved.KubeContext != "" {
		full = append(full, "--context", v.Resolved.KubeContext)
	}
	return append(full, args...)
}

// hostKubeArgs builds a kubectl argv targeting the host cluster, honoring the
// --kubeconfig override and pinning the configured host context.
func (v *VCluster) hostKubeArgs(args ...string) []string {
	full := []string{}
	if v.Kubeconfig != "" {
		full = append(full, "--kubeconfig", v.Kubeconfig)
	}
	if v.Resolved.VCluster.HostContext != "" {
		full = append(full, "--context", v.Resolved.VCluster.HostContext)
	}
	return append(full, args...)
}

// writeValues renders the vcluster values file to a temp path.
func (v *VCluster) writeValues() (string, func(), error) {
	f, err := os.CreateTemp("", "leptostack-vcluster-*.yaml")
	if err != nil {
		return "", func() {}, err
	}
	content := renderVClusterValues(v.Resolved)
	if _, err := f.WriteString(content); err != nil {
		f.Close()
		os.Remove(f.Name())
		return "", func() {}, err
	}
	f.Close()
	return f.Name(), func() { _ = os.Remove(f.Name()) }, nil
}
