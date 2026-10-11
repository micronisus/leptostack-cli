//go:build windows

package portforward

import "os/exec"

// ConfigureDetached is a no-op on Windows.
func ConfigureDetached(cmd *exec.Cmd) {}
