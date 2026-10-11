//go:build unix

package portforward

import (
	"os"
	"strconv"
	"strings"
	"syscall"
)

func bootID() string {
	b, err := os.ReadFile("/proc/sys/kernel/random/boot_id")
	if err != nil {
		return "unknown"
	}
	return strings.TrimSpace(string(b))
}

func processStart(pid int) string {
	b, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	if err != nil {
		return ""
	}
	s := string(b)
	// Skip past the "pid (comm)" prefix which may itself contain spaces/parens.
	i := strings.LastIndex(s, ") ")
	if i < 0 {
		return ""
	}
	fields := strings.Fields(s[i+2:])
	// Field 22 overall is starttime; after removing pid+comm it is index 19.
	if len(fields) <= 19 {
		return ""
	}
	return fields[19]
}

func processAlive(pid int) bool {
	p, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	return p.Signal(syscall.Signal(0)) == nil
}

func killProcess(pid int) error {
	p, err := os.FindProcess(pid)
	if err != nil {
		return err
	}
	return p.Signal(syscall.SIGTERM)
}
