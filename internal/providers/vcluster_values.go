package providers

import (
	"strings"
	"text/template"

	"github.com/micronisus/leptostack-cli/internal/config"
)

// vclusterValuesTemplate mirrors write_vcluster_values from the bash script with
// two optional features: the OIDC sections (the oidc-ca volume and the API
// server --oidc-* extra args) render only when OIDC is enabled, and the whole
// metricsServer integration block is rendered only when metrics are enabled.
// Both default to enabled.
//
// The Corefile inside overwriteConfig contains no template actions, only the
// single braces of the Corefile DSL, so text/template can parse it verbatim.
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
{{ if .OIDC }}    persistence:
      addVolumes:
        - name: oidc-ca
          secret:
            secretName: {{ .ClusterName }}-oidc-ca
      addVolumeMounts:
        - name: oidc-ca
          mountPath: /etc/oidc
          readOnly: true
{{ end }}  coredns:
    overwriteConfig: |-
      .:1053 {
          errors
          health
          ready

          rewrite name regex .*\.nodes\.vcluster\.com kubernetes.default.svc.cluster.local

          rewrite name regex ^((.*)\.)?api\.{{ .ClusterName }}\.internal\.$ apisix-internal-gateway.ingress-{{ .ClusterName }}-internal.svc.cluster.local answer auto
          rewrite name regex ^(.+\.)?{{ .DomainRegex }}\.$ apisix-gateway.ingress-{{ .ClusterName }}.svc.cluster.local answer auto

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
{{ if .OIDC }}        extraArgs:
          - --oidc-issuer-url=https://{{ .LeptostackName }}-sso.{{ .Domain }}/realms/{{ .ClusterName }}
          - --oidc-client-id=che
          - --oidc-username-claim=email
          - --oidc-groups-claim=groups
          - --oidc-ca-file=/etc/oidc/ca.crt
{{ else }}        extraArgs: []
{{ end }}  service:
    enabled: true
    spec:
      type: LoadBalancer
{{ if .Metrics }}integrations:
  metricsServer:
    enabled: true
    apiService:
      service:
        name: rke2-metrics-server
        namespace: kube-system
        port: 443
    nodes: true
    pods: true
{{ end }}# The host ServiceAccount is namespace-scoped (Nexus provisions a RoleBinding to
# the admin ClusterRole, never cluster-scoped grants), so the vCluster must not
# try to create its syncer ClusterRole. Node metrics stay disabled because they
# would require that cluster-scoped access.
rbac:
  clusterRole:
    enabled: false
`

// vclusterValuesTmpl is parsed once; the template is static so a parse error is
// a programming error and panics at init.
var vclusterValuesTmpl = template.Must(template.New("vcluster-values").Parse(vclusterValuesTemplate))

// vclusterValuesData are the inputs to the values template.
type vclusterValuesData struct {
	ClusterName    string
	LeptostackName string
	Domain         string
	DomainRegex    string
	OIDC           bool
	Metrics        bool
}

// renderVClusterValues renders the vcluster values file for a resolved cluster,
// honoring the OIDC and metrics feature toggles.
func renderVClusterValues(r *config.Resolved) string {
	data := vclusterValuesData{
		ClusterName:    r.ClusterName,
		LeptostackName: r.LeptostackName,
		Domain:         r.Domain,
		DomainRegex:    strings.ReplaceAll(r.Domain, ".", `\.`),
		OIDC:           r.VCluster.OIDCEnabled(),
		Metrics:        r.VCluster.MetricsEnabled(),
	}
	var buf strings.Builder
	// Execute only errors when the writer fails; strings.Builder never does.
	_ = vclusterValuesTmpl.Execute(&buf, data)
	return buf.String()
}
