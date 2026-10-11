//go:build windows

package portforward

import (
	"os"
)

func bootID() string { return "" }

func processStart(pid int) string { return "" }

func processAlive(pid int) bool {
	p, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	// On Windows FindProcess always succeeds; assume alive if a state file exists.
	_ = p
	return true
}

func killProcess(pid int) error {
	p, err := os.FindProcess(pid)
	if err != nil {
		return err
	}
	return p.Kill()
}
