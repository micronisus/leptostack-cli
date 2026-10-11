// Package registry pushes and removes module image registry credentials in
// OpenBao at the fixed path static/common/registry, using short-lived
// in-cluster Jobs (the same mechanism as the bash implementation).
package registry

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/micronisus/leptostack-cli/internal/cli"
	"github.com/micronisus/leptostack-cli/internal/kubernetes"
	"github.com/micronisus/leptostack-cli/internal/yamlquote"
)

const (
	jobName    = "leptostack-registry-credentials"
	secretName = "leptostack-registry-credentials"
	kvPath     = "k8s/static/common/registry"
)

// Manager performs registry credential operations for one cluster.
type Manager struct {
	Kube      *kubernetes.Client
	IO        *cli.IOStreams
	Cluster   string
	Namespace string
	Service   string
	Timeout   time.Duration
}

// New builds a Manager for a cluster name.
func New(kube *kubernetes.Client, io *cli.IOStreams, cluster string) *Manager {
	return &Manager{
		Kube:      kube,
		IO:        io,
		Cluster:   cluster,
		Namespace: cluster + "-openbao",
		Service:   cluster + "-openbao-openbao",
		Timeout:   300 * time.Second,
	}
}

// Push stores repository -> credentials at the OpenBao path.
func (m *Manager) Push(ctx context.Context, repository, credentials string) error {
	script := `set -eu
bao kv put "` + kvPath + `" "${MODULE_REGISTRY_REPOSITORY}=${MODULE_REGISTRY_CREDENTIALS}"`
	return m.runJob(ctx, "Storing registry credentials in OpenBao...", script, repository, credentials)
}

// Remove deletes the stored registry credentials.
func (m *Manager) Remove(ctx context.Context) error {
	script := `set -eu
bao kv delete "` + kvPath + `"`
	return m.runJob(ctx, "Removing registry credentials from OpenBao...", script, "", "")
}

func (m *Manager) runJob(ctx context.Context, headline, script, repository, credentials string) error {
	secretManifest := m.secretManifest(repository, credentials)
	jobManifest := m.jobManifest(script)

	m.cleanup(ctx)

	if m.IO != nil {
		m.IO.Errln(headline)
	}
	if err := m.Kube.ApplyJSON(ctx, secretManifest); err != nil {
		return err
	}
	// Ensure the plaintext credential Secret is removed on every path once it
	// exists, mirroring the bash implementation's EXIT trap.
	defer m.cleanup(ctx)

	if err := m.Kube.ApplyJSON(ctx, jobManifest); err != nil {
		return err
	}

	if err := m.Kube.Run(ctx, "wait", "-n", m.Namespace, "--for=condition=complete", "job/"+jobName, "--timeout="+m.Timeout.String()); err != nil {
		logs, _ := m.Kube.OutputCombined(ctx, "logs", "-n", m.Namespace, "job/"+jobName, "--all-containers", "--tail=50")
		return fmt.Errorf("registry credentials job did not complete: %w\n%s", err, logs)
	}

	if m.IO != nil {
		m.IO.Errln("Registry credentials stored successfully.")
	}
	return nil
}

func (m *Manager) cleanup(ctx context.Context) {
	_ = m.Kube.RunDiscard(ctx, "delete", "job", jobName, "-n", m.Namespace, "--ignore-not-found")
	_ = m.Kube.RunDiscard(ctx, "delete", "secret", secretName, "-n", m.Namespace, "--ignore-not-found")
}

func (m *Manager) secretManifest(repository, credentials string) string {
	var b strings.Builder
	b.WriteString("apiVersion: v1\nkind: Secret\nmetadata:\n")
	fmt.Fprintf(&b, "  name: %s\n  namespace: %s\n", secretName, m.Namespace)
	b.WriteString("  labels:\n    app.kubernetes.io/name: leptostack-registry-credentials\ntype: Opaque\nstringData:\n")
	fmt.Fprintf(&b, "  repository: %s\n", yamlQuote(repository))
	fmt.Fprintf(&b, "  credentials: %s\n", yamlQuote(credentials))
	return b.String()
}

func (m *Manager) jobManifest(script string) string {
	indented := indent(script, "              ")
	var b strings.Builder
	b.WriteString("apiVersion: batch/v1\nkind: Job\nmetadata:\n")
	fmt.Fprintf(&b, "  name: %s\n  namespace: %s\n", jobName, m.Namespace)
	b.WriteString("  labels:\n    app.kubernetes.io/name: leptostack-registry-credentials\nspec:\n")
	b.WriteString("  backoffLimit: 3\n  activeDeadlineSeconds: 300\n  ttlSecondsAfterFinished: 300\n")
	b.WriteString("  template:\n    metadata:\n      labels:\n        app.kubernetes.io/name: leptostack-registry-credentials\n")
	b.WriteString("    spec:\n      restartPolicy: Never\n      automountServiceAccountToken: false\n")
	b.WriteString("      securityContext:\n        runAsNonRoot: true\n        runAsUser: 1000\n        runAsGroup: 1000\n")
	b.WriteString("        seccompProfile:\n          type: RuntimeDefault\n")
	b.WriteString("      containers:\n        - name: registry-credentials\n          image: openbao/openbao:latest\n")
	b.WriteString("          imagePullPolicy: IfNotPresent\n          securityContext:\n            allowPrivilegeEscalation: false\n")
	b.WriteString("            readOnlyRootFilesystem: true\n            capabilities:\n              drop:\n                - ALL\n")
	b.WriteString("          env:\n")
	fmt.Fprintf(&b, "            - name: BAO_ADDR\n              value: \"http://%s.%s.svc.cluster.local:8200\"\n", m.Service, m.Namespace)
	b.WriteString("            - name: BAO_TOKEN\n              valueFrom:\n                secretKeyRef:\n                  name: openbao-init\n                  key: root_token\n")
	b.WriteString("            - name: HOME\n              value: /tmp\n")
	b.WriteString("            - name: MODULE_REGISTRY_REPOSITORY\n              valueFrom:\n                secretKeyRef:\n")
	fmt.Fprintf(&b, "                  name: %s\n                  key: repository\n", secretName)
	b.WriteString("            - name: MODULE_REGISTRY_CREDENTIALS\n              valueFrom:\n                secretKeyRef:\n")
	fmt.Fprintf(&b, "                  name: %s\n                  key: credentials\n", secretName)
	b.WriteString("          volumeMounts:\n            - name: tmp\n              mountPath: /tmp\n")
	b.WriteString("          command:\n            - sh\n            - -c\n            - |\n")
	b.WriteString(indented)
	b.WriteString("\n      volumes:\n        - name: tmp\n          emptyDir: {}\n")
	return b.String()
}

func yamlQuote(s string) string {
	return yamlquote.Quote(s)
}

func indent(s, prefix string) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	for i, l := range lines {
		lines[i] = prefix + l
	}
	return strings.Join(lines, "\n")
}
