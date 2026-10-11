package cli

import (
	"encoding/json"
	"fmt"
	"strings"

	"sigs.k8s.io/yaml"
)

// Output format identifiers.
const (
	OutputHuman = "human"
	OutputWide  = "wide"
	OutputJSON  = "json"
	OutputYAML  = "yaml"
	OutputName  = "name"
)

// Format returns the normalized output format.
func (a *App) Format() string {
	f := strings.ToLower(strings.TrimSpace(a.Flags.Output))
	switch f {
	case "", OutputHuman:
		return OutputHuman
	case OutputWide:
		return OutputWide
	case OutputJSON:
		return OutputJSON
	case OutputYAML:
		return OutputYAML
	case OutputName:
		return OutputName
	default:
		return f
	}
}

// ValidateOutput rejects unsupported formats.
func (a *App) ValidateOutput(allowed ...string) error {
	f := a.Format()
	if f == OutputHuman {
		return nil
	}
	for _, ok := range allowed {
		if f == ok {
			return nil
		}
	}
	return Exitf(ExitUsage, "unsupported output format %q (allowed: %s)", a.Flags.Output, strings.Join(append([]string{OutputHuman}, allowed...), ", "))
}

// IsMachine reports whether the output is machine-parseable.
func (a *App) IsMachine() bool {
	switch a.Format() {
	case OutputJSON, OutputYAML, OutputName:
		return true
	}
	return false
}

// EncodeJSON writes v as indented JSON to stdout.
func (a *App) EncodeJSON(v any) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(a.IO.Out, string(b))
	return err
}

// EncodeYAML writes v as YAML to stdout.
func (a *App) EncodeYAML(v any) error {
	b, err := yaml.Marshal(v)
	if err != nil {
		return err
	}
	_, err = fmt.Fprint(a.IO.Out, string(b))
	return err
}

// Encode writes v in the requested machine format (json/yaml).
func (a *App) Encode(v any) error {
	switch a.Format() {
	case OutputJSON:
		return a.EncodeJSON(v)
	case OutputYAML:
		return a.EncodeYAML(v)
	default:
		return Exitf(ExitUsage, "Encode called for non-machine format %q", a.Format())
	}
}

// EncodeOrHuman writes machine output or calls human when the format is human
// or wide.
func (a *App) EncodeOrHuman(v any, human func()) error {
	switch a.Format() {
	case OutputJSON:
		return a.EncodeJSON(v)
	case OutputYAML:
		return a.EncodeYAML(v)
	case OutputName:
		return a.writeNamesFrom(v)
	default:
		human()
		return nil
	}
}

// WriteNames prints names one per line for -o name.
func (a *App) WriteNames(names []string) error {
	for _, n := range names {
		if _, err := fmt.Fprintln(a.IO.Out, n); err != nil {
			return err
		}
	}
	return nil
}

func (a *App) writeNamesFrom(v any) error {
	if names, ok := v.([]string); ok {
		return a.WriteNames(names)
	}
	return a.EncodeJSON(v)
}
