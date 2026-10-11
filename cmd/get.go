package cmd

import (
	"context"
	"encoding/base64"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/micronisus/leptostack-cli/internal/cli"
	"github.com/micronisus/leptostack-cli/internal/config"
	"github.com/micronisus/leptostack-cli/internal/kubernetes"
	"github.com/micronisus/leptostack-cli/internal/kustomizations"
	"github.com/micronisus/leptostack-cli/internal/portforward"
	"github.com/micronisus/leptostack-cli/internal/ui"
)

func newGetCmd(app *cli.App) *cobra.Command {
	c := &cobra.Command{
		Use:   "get",
		Short: "Display one or many objects",
		Args:  cobra.NoArgs,
		RunE:  func(cmd *cobra.Command, args []string) error { return cmd.Help() },
	}
	c.AddCommand(
		newGetClustersCmd(app),
		newGetClusterCmd(app),
		newGetKustomizationsCmd(app),
		newGetSourcesCmd(app),
		newGetSecretsCmd(app),
		newGetContextCmd(app),
		newGetPortForwardsCmd(app),
		newGetEventsCmd(app),
	)
	return c
}

type clusterInfo struct {
	Name           string               `json:"name"`
	Current        bool                 `json:"current"`
	Provider       string               `json:"provider"`
	State          string               `json:"state"`
	Context        string               `json:"context"`
	Domain         string               `json:"domain,omitempty"`
	Kustomizations statusKustomizations `json:"kustomizations"`
	PortForwards   int                  `json:"portForwards"`
}

func newGetClustersCmd(app *cli.App) *cobra.Command {
	return &cobra.Command{
		Use:     "clusters",
		Aliases: []string{"cluster"},
		Short:   "List configured clusters",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := app.ValidateOutput(cli.OutputJSON, cli.OutputYAML, cli.OutputWide, cli.OutputName); err != nil {
				return err
			}
			cfg, err := app.Config()
			if err != nil {
				return err
			}
			ctx, cancel := app.Context(clusterWaitTimeout)
			defer cancel()

			var infos []clusterInfo
			for name := range cfg.Clusters {
				r, err := cfg.Resolve(name)
				if err != nil {
					continue
				}
				infos = append(infos, inspectCluster(ctx, app, r, name == cfg.CurrentClusterName()))
			}
			sortClusterInfos(infos, cfg.CurrentClusterName())

			if app.Format() == cli.OutputName {
				names := make([]string, 0, len(infos))
				for _, ci := range infos {
					names = append(names, ci.Name)
				}
				return app.WriteNames(names)
			}

			return output(app, infos, func() {
				color := app.Colorizer()
				t := ui.NewTable("", "NAME", "PROVIDER", "STATE", "CONTEXT", "DOMAIN", "READY", "PORT-FORWARDS")
				for _, ci := range infos {
					marker := ""
					if ci.Current {
						marker = "*"
					}
					t.AddRow(marker, ci.Name, ci.Provider, color.Status(ci.State), ci.Context, ci.Domain,
						fmt.Sprintf("%d/%d", ci.Kustomizations.Ready, ci.Kustomizations.Total),
						fmt.Sprintf("%d", ci.PortForwards))
				}
				if len(infos) == 0 {
					app.IO.Errln("No clusters configured. Run 'leptostack config init'.")
					return
				}
				t.Render(app.IO.Out)
			})
		},
	}
}

func newGetClusterCmd(app *cli.App) *cobra.Command {
	return &cobra.Command{
		Use:   "cluster",
		Short: "Show the current cluster",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := app.ValidateOutput(cli.OutputJSON, cli.OutputYAML, cli.OutputWide); err != nil {
				return err
			}
			r, err := resolve(app)
			if err != nil {
				return err
			}
			ctx, cancel := app.Context(clusterWaitTimeout)
			defer cancel()
			ci := inspectCluster(ctx, app, r, true)
			return output(app, ci, func() {
				app.IO.Printf("Name:      %s\nProvider:  %s\nState:     %s\nContext:   %s\nDomain:    %s\n",
					ci.Name, ci.Provider, ci.State, ci.Context, ci.Domain)
			})
		},
	}
}

func inspectCluster(ctx context.Context, app *cli.App, r *config.Resolved, current bool) clusterInfo {
	ci := clusterInfo{
		Name: r.Name, Current: current, Provider: r.Provider, Context: r.KubeContext, Domain: r.Domain,
		State: "absent",
	}
	kube := kubeFor(app, r)
	if kube.Healthz(ctx) {
		ci.State = "running"
		if items, err := kustomizations.List(ctx, kube); err == nil {
			ci.Kustomizations.Total = len(items)
			for _, k := range items {
				if k.Ready() && !k.Suspended() {
					ci.Kustomizations.Ready++
				}
				if k.Suspended() {
					ci.Kustomizations.Suspended++
				}
			}
		}
	}
	if states, _ := portforward.ListStates(r.Name); states != nil {
		for _, s := range states {
			if s.Alive() {
				ci.PortForwards++
			}
		}
	}
	return ci
}

func sortClusterInfos(infos []clusterInfo, current string) {
	for i := 1; i < len(infos); i++ {
		for j := i; j > 0 && infos[j-1].Name > infos[j].Name; j-- {
			infos[j-1], infos[j] = infos[j], infos[j-1]
		}
	}
}

func newGetKustomizationsCmd(app *cli.App) *cobra.Command {
	return &cobra.Command{
		Use:     "kustomizations",
		Aliases: []string{"kustomization", "ks"},
		Short:   "List Flux kustomizations",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := app.ValidateOutput(cli.OutputJSON, cli.OutputYAML, cli.OutputWide, cli.OutputName); err != nil {
				return err
			}
			r, err := resolve(app)
			if err != nil {
				return err
			}
			ctx, cancel := app.Context(clusterWaitTimeout)
			defer cancel()
			kube := kubeFor(app, r)
			if err := requireReachable(ctx, app, kube); err != nil {
				return err
			}
			items, err := kustomizations.List(ctx, kube)
			if err != nil {
				return err
			}
			if app.Format() == cli.OutputName {
				names := make([]string, 0, len(items))
				for _, k := range items {
					names = append(names, k.Metadata.Name)
				}
				return app.WriteNames(names)
			}
			return output(app, items, func() {
				color := app.Colorizer()
				t := ui.NewTable("NAME", "READY", "SUSPENDED", "AGE", "MESSAGE")
				for _, k := range items {
					t.AddRow(k.Metadata.Name,
						color.Status(fmt.Sprintf("%t", k.Ready())),
						fmt.Sprintf("%t", k.Suspended()),
						kubernetes.Age(k.Metadata.CreationTimestamp),
						truncate(k.Message(), 60))
				}
				t.Render(app.IO.Out)
			})
		},
	}
}

func newGetSourcesCmd(app *cli.App) *cobra.Command {
	var typeFilter, namespace string
	c := &cobra.Command{
		Use:     "sources",
		Aliases: []string{"source"},
		Short:   "List Flux sources",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := app.ValidateOutput(cli.OutputJSON, cli.OutputYAML, cli.OutputWide, cli.OutputName); err != nil {
				return err
			}
			r, err := resolve(app)
			if err != nil {
				return err
			}
			if namespace == "" {
				namespace = defaultNamespace(app)
			}
			ctx, cancel := app.Context(clusterWaitTimeout)
			defer cancel()
			kube := kubeFor(app, r)
			if err := requireReachable(ctx, app, kube); err != nil {
				return err
			}
			items, err := listSources(ctx, kube, namespace, typeFilter)
			if err != nil {
				return err
			}
			if app.Format() == cli.OutputName {
				names := make([]string, 0, len(items))
				for _, s := range items {
					names = append(names, s.Metadata.Name)
				}
				return app.WriteNames(names)
			}
			return output(app, items, func() {
				color := app.Colorizer()
				t := ui.NewTable("KIND", "NAME", "URL", "REF", "SECRET", "READY", "AGE")
				for _, s := range items {
					t.AddRow(s.Kind, s.Metadata.Name, s.Location(), s.RefString(), s.SecretRefName(),
						color.Status(fmt.Sprintf("%t", s.Ready())), kubernetes.Age(s.Metadata.CreationTimestamp))
				}
				t.Render(app.IO.Out)
			})
		},
	}
	c.Flags().StringVar(&typeFilter, "type", "", "Filter by source type: git, helm, bucket or oci")
	c.Flags().StringVarP(&namespace, "namespace", "n", "", "Namespace scope (default: flux-system for the current cluster)")
	return c
}

var sourceKinds = []struct {
	Type     string
	Resource string
	Kind     string
}{
	{"git", "gitrepositories.source.toolkit.fluxcd.io", "GitRepository"},
	{"helm", "helmrepositories.source.toolkit.fluxcd.io", "HelmRepository"},
	{"bucket", "buckets.source.toolkit.fluxcd.io", "Bucket"},
	{"oci", "ocirepositories.source.toolkit.fluxcd.io", "OCIRepository"},
}

func listSources(ctx context.Context, kube *kubernetes.Client, namespace, typeFilter string) ([]kubernetes.Source, error) {
	var out []kubernetes.Source
	for _, k := range sourceKinds {
		if typeFilter != "" && !strings.EqualFold(typeFilter, k.Type) {
			continue
		}
		var list kubernetes.SourceList
		if err := kube.GetJSON(ctx, &list, k.Resource, "-n", namespace); err != nil {
			// A missing CRD/source kind is not fatal.
			continue
		}
		for i := range list.Items {
			list.Items[i].Kind = k.Kind
			out = append(out, list.Items[i])
		}
	}
	return out, nil
}

type redactedSecret struct {
	Name       string            `json:"name"`
	Namespace  string            `json:"namespace"`
	Type       string            `json:"type"`
	Keys       []string          `json:"keys"`
	Data       map[string]string `json:"data,omitempty"`
	Redacted   bool              `json:"redacted"`
	Age        string            `json:"age,omitempty"`
	CreationTS string            `json:"creationTimestamp,omitempty"`
}

func newGetSecretsCmd(app *cli.App) *cobra.Command {
	var namespace string
	var showValues bool
	c := &cobra.Command{
		Use:   "secrets",
		Short: "List Kubernetes secrets (values redacted by default)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := app.ValidateOutput(cli.OutputJSON, cli.OutputYAML, cli.OutputWide, cli.OutputName); err != nil {
				return err
			}
			r, err := resolve(app)
			if err != nil {
				return err
			}
			if namespace == "" {
				namespace = defaultNamespace(app)
			}
			ctx, cancel := app.Context(clusterWaitTimeout)
			defer cancel()
			kube := kubeFor(app, r)
			if err := requireReachable(ctx, app, kube); err != nil {
				return err
			}
			var list kubernetes.SecretList
			if err := kube.GetJSON(ctx, &list, "secrets", "-n", namespace); err != nil {
				return err
			}

			results := make([]redactedSecret, 0, len(list.Items))
			for _, s := range list.Items {
				rs := redactedSecret{
					Name: s.Metadata.Name, Namespace: s.Metadata.Namespace, Type: s.Type,
					Redacted: !showValues, CreationTS: s.Metadata.CreationTimestamp,
					Age: kubernetes.Age(s.Metadata.CreationTimestamp),
				}
				decoded := map[string]string{}
				for k, v := range s.Data {
					rs.Keys = append(rs.Keys, k)
					if showValues {
						if raw, err := base64.StdEncoding.DecodeString(v); err == nil {
							decoded[k] = string(raw)
						} else {
							decoded[k] = v
						}
					}
				}
				for k, v := range s.StringData {
					rs.Keys = append(rs.Keys, k)
					if showValues {
						decoded[k] = v
					}
				}
				if showValues {
					rs.Data = decoded
				}
				sortStrings(rs.Keys)
				results = append(results, rs)
			}

			if app.Format() == cli.OutputName {
				names := make([]string, 0, len(results))
				for _, s := range results {
					names = append(names, s.Name)
				}
				return app.WriteNames(names)
			}
			return output(app, results, func() {
				t := ui.NewTable("NAME", "TYPE", "KEYS", "AGE")
				for _, s := range results {
					t.AddRow(s.Name, s.Type, strings.Join(s.Keys, ","), s.Age)
				}
				t.Render(app.IO.Out)
			})
		},
	}
	c.Flags().StringVarP(&namespace, "namespace", "n", "", "Namespace scope (default: flux-system for the current cluster)")
	c.Flags().BoolVar(&showValues, "show-values", false, "Reveal secret values")
	return c
}

func newGetContextCmd(app *cli.App) *cobra.Command {
	return &cobra.Command{
		Use:   "context",
		Short: "Print the kubectl context name for the selected cluster",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := app.ValidateOutput(cli.OutputJSON, cli.OutputYAML, cli.OutputName); err != nil {
				return err
			}
			r, err := resolve(app)
			if err != nil {
				return err
			}
			if app.Format() == cli.OutputName || app.Format() == cli.OutputHuman {
				app.IO.Println(r.KubeContext)
				return nil
			}
			return app.Encode(map[string]string{"name": r.KubeContext})
		},
	}
}

type portForwardInfo struct {
	Service   string                `json:"service"`
	Cluster   string                `json:"cluster"`
	Context   string                `json:"context"`
	PID       int                   `json:"pid"`
	Mappings  []portforward.Mapping `json:"mappings"`
	Alive     bool                  `json:"alive"`
	StartedAt string                `json:"startedAt,omitempty"`
	LogFile   string                `json:"logFile,omitempty"`
}

func newGetPortForwardsCmd(app *cli.App) *cobra.Command {
	return &cobra.Command{
		Use:     "port-forwards",
		Aliases: []string{"port-forward", "pf"},
		Short:   "List port-forwards for the selected cluster",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := app.ValidateOutput(cli.OutputJSON, cli.OutputYAML, cli.OutputWide, cli.OutputName); err != nil {
				return err
			}
			r, err := resolve(app)
			if err != nil {
				return err
			}
			states, err := portforward.ListStates(r.Name)
			if err != nil {
				return err
			}
			infos := make([]portForwardInfo, 0, len(states))
			for i := range states {
				s := states[i]
				infos = append(infos, portForwardInfo{
					Service: s.Service, Cluster: s.Cluster, Context: s.Context, PID: s.PID,
					Mappings: s.Mappings, Alive: s.Alive(), StartedAt: s.StartedAt, LogFile: s.LogFile,
				})
			}
			if app.Format() == cli.OutputName {
				names := make([]string, 0, len(infos))
				for _, i := range infos {
					names = append(names, i.Service)
				}
				return app.WriteNames(names)
			}
			return output(app, infos, func() {
				color := app.Colorizer()
				t := ui.NewTable("SERVICE", "ALIVE", "PID", "PORTS", "LOG")
				for _, i := range infos {
					t.AddRow(i.Service, color.Status(fmt.Sprintf("%t", i.Alive)), fmt.Sprintf("%d", i.PID), mappingsString(i.Mappings), i.LogFile)
				}
				if len(infos) == 0 {
					app.IO.Errln("No port-forwards.")
					return
				}
				t.Render(app.IO.Out)
			})
		},
	}
}

func mappingsString(ms []portforward.Mapping) string {
	parts := make([]string, 0, len(ms))
	for _, m := range ms {
		parts = append(parts, fmt.Sprintf("%d:%d", m.Local, m.Remote))
	}
	return strings.Join(parts, ",")
}

func newGetEventsCmd(app *cli.App) *cobra.Command {
	var watch bool
	c := &cobra.Command{
		Use:   "events",
		Short: "Show (or watch) cluster events",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			r, err := resolve(app)
			if err != nil {
				return err
			}
			ctx, cancel := app.Context(0)
			defer cancel()
			kube := kubeFor(app, r)
			argv := []string{"events", "-A"}
			if watch {
				argv = append(argv, "-w")
			}
			return kube.Run(ctx, argv...)
		},
	}
	c.Flags().BoolVarP(&watch, "watch", "w", false, "Watch events in real-time")
	return c
}

func defaultNamespace(app *cli.App) string {
	base := "flux-system"
	if app.Flags.Namespace != "" {
		return app.Flags.Namespace
	}
	return base
}

func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j-1] > s[j]; j-- {
			s[j-1], s[j] = s[j], s[j-1]
		}
	}
}
