// Package yamlquote renders a Go string as a safe YAML double-quoted scalar.
// It is shared by all in-process manifest builders so quoting cannot drift
// between the secret, source and registry generators.
package yamlquote

import "strings"

// Quote returns s as a double-quoted YAML scalar with backslash, quote,
// newline, carriage-return and tab escaped.
func Quote(s string) string {
	replacer := strings.NewReplacer(
		`\`, `\\`,
		`"`, `\"`,
		"\n", `\n`,
		"\r", `\r`,
		"\t", `\t`,
	)
	return `"` + replacer.Replace(s) + `"`
}
