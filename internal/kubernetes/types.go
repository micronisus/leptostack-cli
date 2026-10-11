package kubernetes

import "time"

// ObjectMeta is the common metadata subset used for tables.
type ObjectMeta struct {
	Name              string            `json:"name"`
	Namespace         string            `json:"namespace"`
	Labels            map[string]string `json:"labels,omitempty"`
	CreationTimestamp string            `json:"creationTimestamp,omitempty"`
}

// Condition is a standard Kubernetes status condition.
type Condition struct {
	Type               string `json:"type"`
	Status             string `json:"status"`
	Reason             string `json:"reason,omitempty"`
	Message            string `json:"message,omitempty"`
	LastTransitionTime string `json:"lastTransitionTime,omitempty"`
}

// ReadyStatus returns the Ready condition status, or "Unknown".
func ReadyStatus(conds []Condition) string {
	for _, c := range conds {
		if c.Type == "Ready" {
			return c.Status
		}
	}
	return "Unknown"
}

// ReadyMessage returns the Ready condition message.
func ReadyMessage(conds []Condition) string {
	for _, c := range conds {
		if c.Type == "Ready" {
			return c.Message
		}
	}
	return ""
}

// Kustomization is a Flux Kustomization.
type Kustomization struct {
	Metadata ObjectMeta `json:"metadata"`
	Spec     struct {
		Suspend   bool   `json:"suspend"`
		Path      string `json:"path"`
		Interval  string `json:"interval"`
		SourceRef struct {
			Kind      string `json:"kind"`
			Name      string `json:"name"`
			Namespace string `json:"namespace"`
		} `json:"sourceRef"`
	} `json:"spec"`
	Status struct {
		Conditions            []Condition `json:"conditions"`
		LastAppliedRevision   string      `json:"lastAppliedRevision,omitempty"`
		LastAttemptedRevision string      `json:"lastAttemptedRevision,omitempty"`
	} `json:"status"`
}

// Ready reports whether the kustomization's Ready condition is True.
func (k Kustomization) Ready() bool { return ReadyStatus(k.Status.Conditions) == "True" }

// Suspended reports whether reconciliation is suspended.
func (k Kustomization) Suspended() bool { return k.Spec.Suspend }

// State returns a compact state word for tables.
func (k Kustomization) State() string {
	if k.Suspended() {
		return "Suspended"
	}
	return ReadyStatus(k.Status.Conditions)
}

// Message returns the Ready condition message.
func (k Kustomization) Message() string { return ReadyMessage(k.Status.Conditions) }

// KustomizationList is a list of Flux Kustomizations.
type KustomizationList struct {
	Items []Kustomization `json:"items"`
}

// SourceSpec is a union of the fields the CLI displays for Flux sources.
type SourceSpec struct {
	URL        string `json:"url,omitempty"`
	Endpoint   string `json:"endpoint,omitempty"`
	Interval   string `json:"interval,omitempty"`
	BucketName string `json:"bucketName,omitempty"`
	Provider   string `json:"provider,omitempty"`
	Insecure   bool   `json:"insecure,omitempty"`
	SecretRef  *struct {
		Name string `json:"name"`
	} `json:"secretRef,omitempty"`
	Ref *struct {
		Branch string `json:"branch,omitempty"`
		Tag    string `json:"tag,omitempty"`
		Semver string `json:"semver,omitempty"`
		Name   string `json:"name,omitempty"`
	} `json:"ref,omitempty"`
}

// Source is a Flux source object of any kind.
type Source struct {
	Kind     string     `json:"kind,omitempty"`
	Metadata ObjectMeta `json:"metadata"`
	Spec     SourceSpec `json:"spec"`
	Status   struct {
		Conditions []Condition `json:"conditions"`
	} `json:"status"`
}

// Ready reports whether the source's Ready condition is True.
func (s Source) Ready() bool { return ReadyStatus(s.Status.Conditions) == "True" }

// SecretRefName returns the referenced secret name, if any.
func (s Source) SecretRefName() string {
	if s.Spec.SecretRef == nil {
		return ""
	}
	return s.Spec.SecretRef.Name
}

// Location returns the source URL or bucket endpoint.
func (s Source) Location() string {
	if s.Spec.URL != "" {
		return s.Spec.URL
	}
	return s.Spec.Endpoint
}

// RefString returns a human reference (branch/tag/semver/name).
func (s Source) RefString() string {
	if s.Spec.Ref == nil {
		return ""
	}
	switch {
	case s.Spec.Ref.Branch != "":
		return "branch=" + s.Spec.Ref.Branch
	case s.Spec.Ref.Tag != "":
		return "tag=" + s.Spec.Ref.Tag
	case s.Spec.Ref.Semver != "":
		return "semver=" + s.Spec.Ref.Semver
	case s.Spec.Ref.Name != "":
		return "name=" + s.Spec.Ref.Name
	}
	return ""
}

// SourceList is a list of Flux sources.
type SourceList struct {
	Items []Source `json:"items"`
}

// Secret is a Kubernetes Secret (data values are base64-encoded).
type Secret struct {
	Metadata   ObjectMeta        `json:"metadata"`
	Type       string            `json:"type"`
	Data       map[string]string `json:"data,omitempty"`
	StringData map[string]string `json:"stringData,omitempty"`
}

// SecretList is a list of Secrets.
type SecretList struct {
	Items []Secret `json:"items"`
}

// Age returns a human age like kubectl, based on a creation timestamp.
func Age(ts string) string {
	if ts == "" {
		return "<unknown>"
	}
	t, err := time.Parse(time.RFC3339, ts)
	if err != nil {
		return "<unknown>"
	}
	d := time.Since(t)
	return HumanDuration(d)
}

// HumanDuration renders a duration compactly (e.g. 3d4h, 5m, 12s).
func HumanDuration(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	switch {
	case d >= 24*time.Hour:
		days := int(d / (24 * time.Hour))
		hours := int((d % (24 * time.Hour)) / time.Hour)
		return itoa(days) + "d" + itoa(hours) + "h"
	case d >= time.Hour:
		return itoa(int(d/time.Hour)) + "h" + itoa(int((d%time.Hour)/time.Minute)) + "m"
	case d >= time.Minute:
		return itoa(int(d/time.Minute)) + "m" + itoa(int((d%time.Minute)/time.Second)) + "s"
	default:
		return itoa(int(d/time.Second)) + "s"
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}
