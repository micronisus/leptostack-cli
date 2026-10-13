package providers

import (
	"context"
	"io"
	"reflect"
	"strings"
	"testing"

	"github.com/micronisus/leptostack-cli/internal/cli"
	"github.com/micronisus/leptostack-cli/internal/config"
	"github.com/micronisus/leptostack-cli/internal/execx"
	"github.com/micronisus/leptostack-cli/internal/testutil"
)

func testVCluster(kubeconfig string) *VCluster {
	return &VCluster{Deps: Deps{
		Resolved: &config.Resolved{
			Name:        "dev-alice",
			ClusterName: "dev-alice",
			KubeContext: "vcluster_dev-alice_alice-che_che-ctx",
			VCluster: config.VCluster{
				HostContext:   "che-ctx",
				HostNamespace: "alice-che",
			},
		},
		Kubeconfig: kubeconfig,
	}}
}

func TestHostKubeArgsPrependsKubeconfigAndHostContext(t *testing.T) {
	v := testVCluster("/tmp/che.yaml")
	got := v.hostKubeArgs("-n", "alice-che", "get", "statefulset")
	want := []string{"--kubeconfig", "/tmp/che.yaml", "--context", "che-ctx", "-n", "alice-che", "get", "statefulset"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("hostKubeArgs = %v, want %v", got, want)
	}
}

func TestHostKubeArgsOmitsEmptyKubeconfig(t *testing.T) {
	v := testVCluster("")
	got := v.hostKubeArgs("get", "namespace")
	want := []string{"--context", "che-ctx", "get", "namespace"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("hostKubeArgs = %v, want %v", got, want)
	}
}

func TestKubeArgsUsesVClusterContext(t *testing.T) {
	v := testVCluster("/tmp/che.yaml")
	got := v.kubeArgs("apply", "-f", "-")
	want := []string{
		"--kubeconfig", "/tmp/che.yaml",
		"--context", "vcluster_dev-alice_alice-che_che-ctx",
		"apply", "-f", "-",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("kubeArgs = %v, want %v", got, want)
	}
}

func TestVclusterEnvOnlyWhenKubeconfigSet(t *testing.T) {
	if env := testVCluster("").vclusterEnv(); env != nil {
		t.Fatalf("expected nil env without a kubeconfig, got %v", env)
	}
	env := testVCluster("/tmp/che.yaml").vclusterEnv()
	if env["KUBECONFIG"] != "/tmp/che.yaml" {
		t.Fatalf("KUBECONFIG = %q", env["KUBECONFIG"])
	}
}

func TestVclusterCmdCarriesKubeconfigEnvNotFlag(t *testing.T) {
	v := testVCluster("/tmp/che.yaml")
	cmd := v.vclusterCmd("create", "dev-alice", "--values", "/tmp/values.yaml")
	if cmd.Name != "vcluster" {
		t.Fatalf("name = %q", cmd.Name)
	}
	if cmd.Env["KUBECONFIG"] != "/tmp/che.yaml" {
		t.Fatalf("KUBECONFIG env = %q", cmd.Env["KUBECONFIG"])
	}
	for _, a := range cmd.Args {
		if a == "--kubeconfig" {
			t.Fatalf("vcluster 0.37.x has no --kubeconfig flag; got argv %v", cmd.Args)
		}
	}
	joined := strings.Join(cmd.Args, " ")
	if !strings.Contains(joined, "--namespace alice-che") || !strings.Contains(joined, "--context che-ctx") {
		t.Fatalf("vcluster argv missing host flags: %v", cmd.Args)
	}
}

// TestVClusterKubeconfigPropagation drives Exists and Status through the real
// execx runner against fake vcluster/kubectl shims and asserts the kubeconfig
// reaches every invocation: --kubeconfig for kubectl, KUBECONFIG for vcluster.
func TestVClusterKubeconfigPropagation(t *testing.T) {
	env := testutil.NewFakeEnv(t)
	env.Tool(t, "kubectl", `echo "kubectl $*" >> "$FAKE_LOG"
case "$*" in
  *"config get-contexts -o name"*) echo che-ctx ;;
esac
exit 0`)
	env.Tool(t, "vcluster", `echo "vcluster KUBECONFIG=$KUBECONFIG $*" >> "$FAKE_LOG"; exit 0`)
	env.Active(t)

	streams := cli.NewIOStreams(strings.NewReader(""), io.Discard, io.Discard)
	v := testVCluster("/tmp/che.yaml")
	v.R = execx.New(streams, 0)
	v.IO = streams

	if v.Exists(context.Background()) {
		t.Fatal("Exists should be false when the shim emits no statefulset name")
	}
	if err := v.Status(context.Background()); err != nil {
		t.Fatalf("Status: %v", err)
	}

	inv := env.Invocations(t)
	var sawKubectl, sawVcluster, sawHostContext bool
	for _, line := range inv {
		if strings.HasPrefix(line, "kubectl ") {
			sawKubectl = true
			if !strings.Contains(line, "--kubeconfig /tmp/che.yaml") {
				t.Fatalf("kubectl invocation missing --kubeconfig: %q", line)
			}
			if strings.Contains(line, "get statefulset") {
				if !strings.Contains(line, "--context che-ctx") {
					t.Fatalf("host kubectl invocation missing --context: %q", line)
				}
				sawHostContext = true
			}
		}
		if strings.HasPrefix(line, "vcluster ") {
			sawVcluster = true
			if !strings.Contains(line, "KUBECONFIG=/tmp/che.yaml") {
				t.Fatalf("vcluster invocation missing KUBECONFIG: %q", line)
			}
			if strings.Contains(line, "--kubeconfig") {
				t.Fatalf("vcluster must not receive --kubeconfig: %q", line)
			}
		}
	}
	if !sawKubectl || !sawVcluster || !sawHostContext {
		t.Fatalf("expected kubectl (with host context) and vcluster invocations, got %v", inv)
	}
}
