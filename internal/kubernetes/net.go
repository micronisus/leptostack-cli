package kubernetes

import "strings"

// Subnet100 returns the "<subnet>.100" address used for the minikube MetalLB
// pool and the LeptoStack gateway, given the minikube node IP.
func Subnet100(ip string) string {
	parts := strings.Split(strings.TrimSpace(ip), ".")
	if len(parts) < 3 {
		return ""
	}
	return strings.Join(parts[:3], ".") + ".100"
}
