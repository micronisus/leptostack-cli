package cmd

import (
	"strings"
	"testing"

	"github.com/micronisus/leptostack-cli/internal/cli"
	"github.com/micronisus/leptostack-cli/internal/testutil"
)

// TestDeleteOnlyDeletesCluster guards that `delete` removes the cluster and does
// not recreate it (which would re-run minikube start and the Flux bootstrap).
func TestDeleteOnlyDeletesCluster(t *testing.T) {
	env := newEnv(t)
	testutil.SetHomeConfig(t, testConfig)

	_, errOut, code := runCLI(t, "", "delete", "cluster", "local", "--yes")
	if code != 0 {
		t.Fatalf("exit code = %d (stderr=%s)", code, errOut)
	}
	if !env.HasInvocation(t, "minikube delete -p leptostack-local") {
		t.Fatalf("expected the cluster to be deleted; got %v", env.Invocations(t))
	}
	for _, line := range env.Invocations(t) {
		if strings.Contains(line, "minikube start") {
			t.Fatalf("delete must not recreate the cluster; got %v", env.Invocations(t))
		}
		if strings.Contains(line, "bootstrap") {
			t.Fatalf("delete must not re-bootstrap Flux; got %v", env.Invocations(t))
		}
	}
}

// TestDeleteRequiresSubcommand guards that bare `delete` only prints help and
// does not delete the cluster.
func TestDeleteRequiresSubcommand(t *testing.T) {
	env := newEnv(t)
	testutil.SetHomeConfig(t, testConfig)

	_, errOut, code := runCLI(t, "", "delete")
	if code != 0 {
		t.Fatalf("exit code = %d (stderr=%s)", code, errOut)
	}
	if env.HasInvocation(t, "minikube delete") {
		t.Fatalf("bare delete must not delete the cluster; got %v", env.Invocations(t))
	}
}

// TestDeleteClusterRequiresName guards that `delete cluster` needs an explicit
// cluster name.
func TestDeleteClusterRequiresName(t *testing.T) {
	env := newEnv(t)
	testutil.SetHomeConfig(t, testConfig)

	_, _, code := runCLI(t, "", "delete", "cluster", "--yes")
	if code != cli.ExitUsage {
		t.Fatalf("exit code = %d, want %d", code, cli.ExitUsage)
	}
	if env.HasInvocation(t, "minikube delete") {
		t.Fatalf("missing name must not delete the cluster; got %v", env.Invocations(t))
	}
}

// TestDeleteClusterRejectsClusterFlag guards that -c is not applicable to
// `delete cluster`; the name is positional.
func TestDeleteClusterRejectsClusterFlag(t *testing.T) {
	env := newEnv(t)
	testutil.SetHomeConfig(t, testConfig)

	_, errOut, code := runCLI(t, "", "delete", "cluster", "-c", "local", "--yes")
	if code != cli.ExitUsage {
		t.Fatalf("exit code = %d (stderr=%s), want %d", code, errOut, cli.ExitUsage)
	}
	if env.HasInvocation(t, "minikube delete") {
		t.Fatalf("-c must not delete a cluster; got %v", env.Invocations(t))
	}
}
