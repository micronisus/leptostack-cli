package providers

import (
	"strings"

	"github.com/micronisus/leptostack-cli/internal/config"
)

// vclusterValuesTemplate mirrors write_vcluster_values from the bash script.
const vclusterValuesTemplate = `controlPlane:
  statefulSet:
    resources:
      requests:
        cpu: "2"
        memory: "4Gi"
      limits:
        cpu: "4"
        memory: "8Gi"
        ephemeral-storage: "20Gi"
    persistence:
      addVolumes:
        - name: oidc-ca
          secret:
            secretName: __CLUSTER_NAME__-oidc-ca
      addVolumeMounts:
        - name: oidc-ca
          mountPath: /etc/oidc
          readOnly: true
  coredns:
    overwriteConfig: |-
      .:1053 {
          errors
          health
          ready

          rewrite name regex .*\.nodes\.vcluster\.com kubernetes.default.svc.cluster.local

          rewrite name regex ^((.*)\.)?api\.__CLUSTER_NAME__\.internal\.$ apisix-internal-gateway.ingress-__CLUSTER_NAME__-internal.svc.cluster.local answer auto
          rewrite name regex ^(.+\.)?__LEPTOSTACK_DOMAIN_REGEX__\.$ apisix-gateway.ingress-__CLUSTER_NAME__.svc.cluster.local answer auto

          kubernetes cluster.local in-addr.arpa ip6.arpa {
              pods insecure
              fallthrough in-addr.arpa ip6.arpa
          }

          hosts /etc/coredns/NodeHosts {
              ttl 60
              reload 15s
              fallthrough
          }

          prometheus :9153
          forward . /etc/resolv.conf
          cache 30
          loop
          reload
          loadbalance
      }

      import /etc/coredns/custom/*.server
  distro:
    k8s:
      apiServer:
        extraArgs:
          - --oidc-issuer-url=https://__LEPTOSTACK_NAME__-sso.__LEPTOSTACK_DOMAIN__/realms/__CLUSTER_NAME__
          - --oidc-client-id=che
          - --oidc-username-claim=email
          - --oidc-groups-claim=groups
          - --oidc-ca-file=/etc/oidc/ca.crt
  service:
    enabled: true
    spec:
      type: LoadBalancer
integrations:
  metricsServer:
    enabled: true
    apiService:
      service:
        name: rke2-metrics-server
        namespace: kube-system
        port: 443
    nodes: true
    pods: true
`

// renderVClusterValues substitutes the cluster-specific placeholders.
func renderVClusterValues(r *config.Resolved) string {
	domainRegex := strings.ReplaceAll(r.Domain, ".", `\.`)
	out := vclusterValuesTemplate
	out = strings.ReplaceAll(out, "__CLUSTER_NAME__", r.ClusterName)
	out = strings.ReplaceAll(out, "__LEPTOSTACK_NAME__", r.LeptostackName)
	out = strings.ReplaceAll(out, "__LEPTOSTACK_DOMAIN_REGEX__", domainRegex)
	out = strings.ReplaceAll(out, "__LEPTOSTACK_DOMAIN__", r.Domain)
	return out
}
