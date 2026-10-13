//go:build windows

package ca

// DetectDistro reports the Windows platform for CA trust integration.
func DetectDistro() (string, error) { return "windows", nil }
