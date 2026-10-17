// Package portforward implements the service catalog, foreground/detached
// port-forwarding, and per-cluster state files. Detached forwards record a JSON
// state file under $XDG_RUNTIME_DIR/leptostack/<cluster>/port-forwards/, with
// boot-id and process-start-time validation to avoid stale-PID reuse.
package portforward

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/micronisus/leptostack-cli/internal/config"
	"github.com/micronisus/leptostack-cli/internal/runtime"
)

// Mapping is one local:remote port pair.
type Mapping struct {
	Local  int `json:"local"`
	Remote int `json:"remote"`
}

// Service describes a port-forwardable cluster service.
type Service struct {
	Name      string    `json:"name"`
	Namespace string    `json:"namespace"`
	Resource  string    `json:"resource"`
	Mappings  []Mapping `json:"mappings"`
}

// Catalog returns the service catalog for a resolved cluster.
func Catalog(r *config.Resolved) []Service {
	c := r.ClusterName
	return []Service{
		{Name: "openbao", Namespace: c + "-openbao", Resource: c + "-openbao-openbao", Mappings: []Mapping{{8200, 8200}}},
		{Name: "rabbitmq", Namespace: c + "-rabbitmq", Resource: r.LeptostackName + "-rabbitmq", Mappings: []Mapping{{15671, 15671}}},
		{Name: "postgres", Namespace: c + "-pgcluster", Resource: c + "-pgcluster-rw", Mappings: []Mapping{{5432, 5432}}},
		{Name: "valkey", Namespace: c + "-valkey", Resource: "valkey-" + r.LeptostackName + "-valkey", Mappings: []Mapping{{6379, 6379}}},
		{Name: "flowable", Namespace: c + "-flowable", Resource: "flowable-rest", Mappings: []Mapping{{8080, 8080}}},
		{Name: "greenmail", Namespace: "greenmail", Resource: "api", Mappings: []Mapping{{8025, 80}}},
		{Name: "rustfs", Namespace: c + "-rustfs", Resource: "rustfs-svc", Mappings: []Mapping{{9000, 9000}}},
	}
}

// SupportedNames returns the catalog service names in catalog order.
func SupportedNames(r *config.Resolved) []string {
	out := []string{}
	for _, s := range Catalog(r) {
		out = append(out, s.Name)
	}
	return out
}

// Lookup finds a service by name.
func Lookup(r *config.Resolved, name string) (Service, error) {
	for _, s := range Catalog(r) {
		if s.Name == name {
			return s, nil
		}
	}
	return Service{}, fmt.Errorf("unknown service %q (supported: %v)", name, SupportedNames(r))
}

// PortForwardArgs builds the kubectl port-forward argv for a service.
func (s Service) PortForwardArgs(address string, localOverride int) []string {
	args := []string{"port-forward", "-n", s.Namespace, "services/" + s.Resource}
	if address != "" {
		args = append(args, "--address", address)
	}
	for i, m := range s.Mappings {
		local := m.Local
		if i == 0 && localOverride > 0 {
			local = localOverride
		}
		args = append(args, fmt.Sprintf("%d:%d", local, m.Remote))
	}
	return args
}

// State is the on-disk record for a detached port-forward.
type State struct {
	Service      string    `json:"service"`
	Cluster      string    `json:"cluster"`
	PID          int       `json:"pid"`
	ProcessStart string    `json:"processStart"`
	BootID       string    `json:"bootId"`
	Context      string    `json:"context"`
	Mappings     []Mapping `json:"mappings"`
	LogFile      string    `json:"logFile"`
	StartedAt    string    `json:"startedAt"`
}

// WriteState persists a state record for the cluster.
func WriteState(cluster string, st State) error {
	if _, err := runtime.EnsureClusterDir(cluster); err != nil {
		return err
	}
	if err := os.MkdirAll(runtime.PortForwardDir(cluster), 0o700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(runtime.PortForwardStateFile(cluster, st.Service), b, 0o600)
}

// ReadState reads a state record, returning (nil, nil) if absent.
func ReadState(cluster, service string) (*State, error) {
	b, err := os.ReadFile(runtime.PortForwardStateFile(cluster, service))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var st State
	if err := json.Unmarshal(b, &st); err != nil {
		return nil, err
	}
	return &st, nil
}

// RemoveState deletes a state file.
func RemoveState(cluster, service string) {
	_ = os.Remove(runtime.PortForwardStateFile(cluster, service))
}

// ListStates returns all state records for a cluster, sorted by service name.
func ListStates(cluster string) ([]State, error) {
	entries, err := os.ReadDir(runtime.PortForwardDir(cluster))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var out []State
	for _, e := range entries {
		if e.IsDir() || len(e.Name()) < 6 || e.Name()[len(e.Name())-5:] != ".json" {
			continue
		}
		svc := e.Name()[:len(e.Name())-5]
		st, err := ReadState(cluster, svc)
		if err != nil || st == nil {
			continue
		}
		out = append(out, *st)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Service < out[j].Service })
	return out, nil
}

// Alive reports whether the process recorded in st is still running.
func (st *State) Alive() bool {
	if st == nil || st.PID <= 0 {
		return false
	}
	if st.BootID != "" && st.BootID != bootID() {
		return false
	}
	if st.ProcessStart != "" {
		if cur := processStart(st.PID); cur == "" || cur != st.ProcessStart {
			return false
		}
	}
	return processAlive(st.PID)
}

// Kill terminates the recorded process if it is alive.
func (st *State) Kill() error {
	if !st.Alive() {
		return nil
	}
	return killProcess(st.PID)
}

// NewState builds a state record for the current process.
func NewState(cluster, service, kubeContext, logFile string, mappings []Mapping) State {
	return State{
		Service:      service,
		Cluster:      cluster,
		PID:          os.Getpid(),
		ProcessStart: processStart(os.Getpid()),
		BootID:       bootID(),
		Context:      kubeContext,
		Mappings:     mappings,
		LogFile:      logFile,
		StartedAt:    time.Now().UTC().Format(time.RFC3339),
	}
}

// LegacyStopForwards stops port-forwards started by the bash implementation,
// which recorded "boot_id start_time pid" in <service>.pid under the shared
// legacy state directory. Returns how many were stopped.
func LegacyStopForwards() int {
	dir := runtime.LegacyPortForwardDir()
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0
	}
	stopped := 0
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".pid") {
			continue
		}
		path := filepath.Join(dir, e.Name())
		b, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		fields := strings.Fields(string(b))
		if len(fields) < 3 {
			_ = os.Remove(path)
			continue
		}
		storedBoot, storedStart, storedPID := fields[0], fields[1], fields[2]
		pid, err := strconv.Atoi(storedPID)
		if err != nil {
			_ = os.Remove(path)
			continue
		}
		if storedBoot == bootID() && processStart(pid) == storedStart && processAlive(pid) {
			if err := killProcess(pid); err == nil {
				stopped++
			}
		}
		_ = os.Remove(path)
	}
	return stopped
}

// ParseLocalPort validates a --local-port value.
func ParseLocalPort(s string) (int, error) {
	if s == "" {
		return 0, nil
	}
	n, err := strconv.Atoi(s)
	if err != nil || n < 1 || n > 65535 {
		return 0, errors.New("local port must be between 1 and 65535")
	}
	return n, nil
}
