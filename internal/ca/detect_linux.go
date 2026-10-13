//go:build linux

package ca

import (
	"fmt"
	"os"
)

// DetectDistro reads /etc/os-release and maps it to a supported distro.
func DetectDistro() (string, error) {
	b, err := os.ReadFile("/etc/os-release")
	if err != nil {
		return "", fmt.Errorf("unsupported Linux distribution")
	}
	id := parseOSReleaseID(string(b))
	switch id {
	case "fedora":
		return "fedora", nil
	case "arch":
		return "arch", nil
	}
	return "", fmt.Errorf("unsupported Linux distribution: %s (supported: Fedora, Arch Linux)", id)
}

func parseOSReleaseID(contents string) string {
	for _, line := range splitLines(contents) {
		if len(line) > 3 && line[:3] == "ID=" {
			v := line[3:]
			if len(v) >= 2 && (v[0] == '"' || v[0] == '\'') {
				v = v[1 : len(v)-1]
			}
			return v
		}
	}
	return ""
}

func splitLines(s string) []string {
	var out []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			out = append(out, s[start:i])
			start = i + 1
		}
	}
	if start < len(s) {
		out = append(out, s[start:])
	}
	return out
}
