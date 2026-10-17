package portforward

import (
	"testing"

	"github.com/micronisus/leptostack-cli/internal/config"
)

func resolved() *config.Resolved {
	return &config.Resolved{
		Name:           "local",
		ClusterName:    "local",
		LeptostackName: "portal",
	}
}

func TestCatalog(t *testing.T) {
	names := SupportedNames(resolved())
	want := []string{"openbao", "rabbitmq", "postgres", "valkey", "flowable", "greenmail", "rustfs"}
	if len(names) != len(want) {
		t.Fatalf("catalog = %v want %v", names, want)
	}
	for i := range want {
		if names[i] != want[i] {
			t.Fatalf("catalog[%d] = %q want %q", i, names[i], want[i])
		}
	}
	if _, err := Lookup(resolved(), "nope"); err == nil {
		t.Error("expected unknown service error")
	}
}

func TestPortForwardArgs(t *testing.T) {
	svc, _ := Lookup(resolved(), "postgres")
	args := svc.PortForwardArgs("0.0.0.0", 15432)
	joined := ""
	for _, a := range args {
		joined += a + " "
	}
	for _, want := range []string{"port-forward", "-n local-pgcluster", "services/local-pgcluster-rw", "--address 0.0.0.0", "15432:5432"} {
		if !contains(joined, want) {
			t.Errorf("args missing %q: %s", want, joined)
		}
	}
}

func TestParseLocalPort(t *testing.T) {
	if _, err := ParseLocalPort("70000"); err == nil {
		t.Error("expected range error")
	}
	if n, err := ParseLocalPort("15432"); err != nil || n != 15432 {
		t.Errorf("ParseLocalPort = %d,%v", n, err)
	}
}

func TestStateRoundTrip(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_RUNTIME_DIR", dir)

	st := NewState("local", "postgres", "leptostack-local", "/tmp/x.log", []Mapping{{15432, 5432}})
	if err := WriteState("local", st); err != nil {
		t.Fatal(err)
	}
	got, err := ReadState("local", "postgres")
	if err != nil || got == nil {
		t.Fatalf("ReadState: %v %v", got, err)
	}
	if got.Service != "postgres" || got.PID != st.PID {
		t.Errorf("round-trip mismatch: %+v", got)
	}
	if !got.Alive() {
		t.Error("state for the current process should be alive")
	}
	RemoveState("local", "postgres")
	if again, _ := ReadState("local", "postgres"); again != nil {
		t.Error("state should be removed")
	}
}

func contains(haystack, needle string) bool {
	return len(haystack) >= len(needle) && (func() bool {
		for i := 0; i+len(needle) <= len(haystack); i++ {
			if haystack[i:i+len(needle)] == needle {
				return true
			}
		}
		return false
	})()
}
