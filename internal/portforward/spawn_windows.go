//go:build windows

package portforward

import (
	"os/exec"
	"syscall"

	"golang.org/x/sys/windows"
)

// ConfigureDetached starts the child in its own process group with no console,
// so it survives the parent exiting and can be tracked by its creation time.
func ConfigureDetached(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		CreationFlags: windows.DETACHED_PROCESS | windows.CREATE_NEW_PROCESS_GROUP,
	}
}
