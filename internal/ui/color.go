package ui

// Colorizer applies ANSI colors when enabled.
type Colorizer struct {
	Enabled bool
}

// Color codes.
const (
	reset  = "\033[0m"
	red    = "\033[31m"
	green  = "\033[32m"
	yellow = "\033[33m"
	cyan   = "\033[36m"
	bold   = "\033[1m"
)

func (c Colorizer) wrap(code, s string) string {
	if !c.Enabled {
		return s
	}
	return code + s + reset
}

// Red renders s in red.
func (c Colorizer) Red(s string) string { return c.wrap(red, s) }

// Green renders s in green.
func (c Colorizer) Green(s string) string { return c.wrap(green, s) }

// Yellow renders s in yellow.
func (c Colorizer) Yellow(s string) string { return c.wrap(yellow, s) }

// Cyan renders s in cyan.
func (c Colorizer) Cyan(s string) string { return c.wrap(cyan, s) }

// Bold renders s in bold.
func (c Colorizer) Bold(s string) string { return c.wrap(bold, s) }

// Status colorizes a ready/state string: True/Ready/Running green, error red,
// everything else yellow.
func (c Colorizer) Status(s string) string {
	switch s {
	case "True", "Ready", "Running", "installed", "configured", "Succeeded", "enabled":
		return c.Green(s)
	case "False", "Failed", "Error", "absent", "missing":
		return c.Red(s)
	default:
		return c.Yellow(s)
	}
}
