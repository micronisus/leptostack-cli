//go:build unix

package portforward

import (
	"os/exec"
	"syscall"
)

// ConfigureDetached starts the child in its own session so it survives the
// parent exiting.
func ConfigureDetached(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
}
