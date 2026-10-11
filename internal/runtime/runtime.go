// Package runtime resolves per-cluster runtime state directories (port-forward
// PID/log files and the cached vcluster context) under
// $XDG_RUNTIME_DIR/leptostack/<cluster>/, with the same safety checks as the
// bash implementation's fallback directory.
package runtime

import (
	"fmt"
	"os"
	"path/filepath"
)

// BaseDir returns the per-user runtime root for leptostack.
func BaseDir() string {
	if dir := os.Getenv("XDG_RUNTIME_DIR"); dir != "" {
		return filepath.Join(dir, "leptostack")
	}
	return filepath.Join(os.TempDir(), fmt.Sprintf("leptostack-%d", os.Getuid()))
}

// ClusterDir returns (without creating) the runtime directory for a cluster.
func ClusterDir(cluster string) string {
	return filepath.Join(BaseDir(), sanitize(cluster))
}

// EnsureClusterDir creates and returns the per-cluster runtime directory.
func EnsureClusterDir(cluster string) (string, error) {
	base := BaseDir()
	if err := ensureSafeDir(base); err != nil {
		return "", err
	}
	dir := ClusterDir(cluster)
	if err := ensureSafeDir(dir); err != nil {
		return "", err
	}
	return dir, nil
}

func sanitize(name string) string {
	if name == "" {
		return "default"
	}
	out := make([]rune, 0, len(name))
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_', r == '.':
			out = append(out, r)
		default:
			out = append(out, '_')
		}
	}
	return string(out)
}

// ensureSafeDir creates dir mode 0700, refusing symlinks or directories owned
// by another user.
func ensureSafeDir(dir string) error {
	if info, err := os.Lstat(dir); err == nil {
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("refusing to use symlinked runtime directory: %s", dir)
		}
		if !info.IsDir() {
			return fmt.Errorf("runtime path is not a directory: %s", dir)
		}
		if !ownedByUID(info, os.Getuid()) {
			return fmt.Errorf("refusing to use runtime directory owned by another user: %s", dir)
		}
		return nil
	} else if !os.IsNotExist(err) {
		return err
	}
	return os.MkdirAll(dir, 0o700)
}

// VClusterContextFile returns the cached vcluster context path for a cluster.
func VClusterContextFile(cluster string) string {
	return filepath.Join(ClusterDir(cluster), "vcluster-context")
}

// ReadVClusterContext returns the cached vcluster context name, if any.
func ReadVClusterContext(cluster string) string {
	b, err := os.ReadFile(VClusterContextFile(cluster))
	if err != nil {
		return ""
	}
	return string(trimSpaceBytes(b))
}

// WriteVClusterContext caches the vcluster-generated context name.
func WriteVClusterContext(cluster, context string) error {
	if _, err := EnsureClusterDir(cluster); err != nil {
		return err
	}
	return os.WriteFile(VClusterContextFile(cluster), []byte(context+"\n"), 0o600)
}

// ClearVClusterContext removes the cached vcluster context.
func ClearVClusterContext(cluster string) error {
	err := os.Remove(VClusterContextFile(cluster))
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

// PortForwardDir returns the per-cluster port-forward state directory.
func PortForwardDir(cluster string) string {
	return filepath.Join(ClusterDir(cluster), "port-forwards")
}

// PortForwardStateFile returns the JSON state file for a service's forward.
func PortForwardStateFile(cluster, service string) string {
	return filepath.Join(PortForwardDir(cluster), service+".json")
}

// PortForwardLogFile returns the log file for a service's forward.
func PortForwardLogFile(cluster, service string) string {
	return filepath.Join(PortForwardDir(cluster), service+".log")
}

// LegacyPortForwardDir returns the bash-era shared state directory, used to
// interoperate with background forwards started by the old script.
func LegacyPortForwardDir() string {
	if dir := os.Getenv("XDG_RUNTIME_DIR"); dir != "" {
		return filepath.Join(dir, "leptostack", "port-forwards")
	}
	return filepath.Join(os.TempDir(), fmt.Sprintf("leptostack-port-forwards-%d", os.Getuid()))
}

func trimSpaceBytes(b []byte) []byte {
	start := 0
	for start < len(b) && (b[start] == ' ' || b[start] == '\t' || b[start] == '\n' || b[start] == '\r') {
		start++
	}
	end := len(b)
	for end > start && (b[end-1] == ' ' || b[end-1] == '\t' || b[end-1] == '\n' || b[end-1] == '\r') {
		end--
	}
	return b[start:end]
}
