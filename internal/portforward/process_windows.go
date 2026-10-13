//go:build windows

package portforward

import (
	"fmt"

	"golang.org/x/sys/windows"
)

// bootID has no Windows equivalent. PID reuse is guarded by the process
// creation-time fingerprint returned by processStart, so Alive() compares that
// instead of a boot id.
func bootID() string { return "" }

// processStart returns a stable fingerprint of the process creation time, used
// to detect a recycled PID. It returns "" when the process cannot be opened.
func processStart(pid int) string {
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		return ""
	}
	defer windows.CloseHandle(h)
	var creation, exit, kernel, user windows.Filetime
	if err := windows.GetProcessTimes(h, &creation, &exit, &kernel, &user); err != nil {
		return ""
	}
	return fmt.Sprintf("%d", creation.Nanoseconds())
}

// processAlive reports whether pid names a live process.
func processAlive(pid int) bool {
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		return false
	}
	defer windows.CloseHandle(h)
	var code uint32
	if err := windows.GetExitCodeProcess(h, &code); err != nil {
		return false
	}
	const stillActive = 259 // STILL_ACTIVE
	return code == stillActive
}

// killProcess terminates pid, returning a clear error when the process is gone.
func killProcess(pid int) error {
	h, err := windows.OpenProcess(windows.PROCESS_TERMINATE, false, uint32(pid))
	if err != nil {
		return fmt.Errorf("opening process %d: %w", pid, err)
	}
	defer windows.CloseHandle(h)
	if err := windows.TerminateProcess(h, 1); err != nil {
		return fmt.Errorf("terminating process %d: %w", pid, err)
	}
	return nil
}
