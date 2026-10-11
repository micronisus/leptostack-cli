package gitops

import (
	"fmt"
	"os"
	"strings"
)

func insertAfterMarker(path, marker, newLine string) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	lines := strings.Split(string(b), "\n")
	out := make([]string, 0, len(lines)+1)
	inserted := false
	for _, line := range lines {
		out = append(out, line)
		if !inserted && strings.HasPrefix(strings.TrimSpace(line), marker) {
			out = append(out, newLine)
			inserted = true
		}
	}
	if !inserted {
		return fmt.Errorf("marker %q not found in %s", marker, path)
	}
	return os.WriteFile(path, []byte(strings.Join(out, "\n")), 0o644)
}

func replaceInFile(path, old, new string) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return os.WriteFile(path, []byte(strings.ReplaceAll(string(b), old, new)), 0o644)
}

func replaceLinePrefix(path, prefix, replacement string) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	lines := strings.Split(string(b), "\n")
	for i, line := range lines {
		if strings.HasPrefix(line, prefix) {
			lines[i] = replacement
		}
	}
	return os.WriteFile(path, []byte(strings.Join(lines, "\n")), 0o644)
}

func appendString(path, s string) error {
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.WriteString(s)
	return err
}

// minikubeClusterConfig renders infrastructure/cluster-config.yaml for minikube.
func minikubeClusterConfig(clusterName, domain, minikubeIP string) string {
	if minikubeIP == "" {
		minikubeIP = "192.168.49.2"
	}
	return fmt.Sprintf(`---
apiVersion: v1
kind: ConfigMap
metadata:
  name: config
  namespace: metallb-system
data:
  config: |
    address-pools:
    - name: default
      protocol: layer2
      addresses:
      - 192.168.49.100-192.168.49.110
---
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  annotations:
    cert-manager.io/cluster-issuer: internal-issuer
    k8s.apisix.apache.org/plugin-config-name: %s-plugin-config
  name: registry-ingress
  namespace: kube-system
spec:
  ingressClassName: %s-apisix
  tls:
    - hosts:
        - registry.%s
      secretName: registry-tls
  rules:
    - host: registry.%s
      http:
        paths:
          - backend:
              service:
                name: registry
                port:
                  number: 80
            path: /
            pathType: Prefix
---
apiVersion: crd.projectcalico.org/v1
kind: GlobalNetworkPolicy
metadata:
  name: default-deny
spec:
  namespaceSelector: has(kubernetes.io/metadata.name) && kubernetes.io/metadata.name not in {"kube-system"}
  types:
    - Ingress
    - Egress
  egress:
    - action: Allow
---
apiVersion: v1
kind: ConfigMap
metadata:
  name: coredns
  namespace: kube-system
data:
  Corefile: |
    .:53 {
        log
        errors
        health {
           lameduck 5s
        }
        ready
        rewrite name regex ^((.*)\.)?api\.%s\.internal\.$ apisix-internal-gateway.ingress-%s-internal.svc.cluster.local answer auto
        kubernetes cluster.local in-addr.arpa ip6.arpa {
           pods insecure
           fallthrough in-addr.arpa ip6.arpa
           ttl 30
        }
        prometheus :9153
        hosts {
           %s host.minikube.internal
           fallthrough
        }
        forward . /etc/resolv.conf {
           max_concurrent 1000
        }
        cache 30 {
           disable success cluster.local
           disable denial cluster.local
        }
        loop
        reload
        loadbalance
    }
`, clusterName, clusterName, domain, domain, clusterName, clusterName, minikubeIP)
}
