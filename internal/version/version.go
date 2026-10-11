// Package version holds build information for the leptostack binary.
package version

import (
	"fmt"
	"runtime"
	"strings"
)

// These variables are injected at build time via -ldflags.
var (
	Version = "dev"
	Commit  = "none"
	Date    = "unknown"
)

// Info describes the running build.
type Info struct {
	Version   string `json:"version"`
	Commit    string `json:"commit"`
	Date      string `json:"date"`
	GoVersion string `json:"goVersion"`
	Platform  string `json:"platform"`
}

// Get returns the build information for the running binary.
func Get() Info {
	return Info{
		Version:   Version,
		Commit:    Commit,
		Date:      Date,
		GoVersion: runtime.Version(),
		Platform:  runtime.GOOS + "/" + runtime.GOARCH,
	}
}

// Semver is a minimal semantic-version comparator that mirrors `sort -V` for
// the shapes of versions this tool deals with (optional leading "v", dotted
// numeric components, optional pre-release suffix).
type Semver struct {
	parts []int
	pre   string
}

// ParseSemver parses a version string into a comparable value.
func ParseSemver(s string) Semver {
	s = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(s), "v"))
	// Split off pre-release/build metadata; for ordering purposes a pre-release
	// sorts before the corresponding release.
	pre := ""
	for i, r := range s {
		if r == '-' || r == '+' {
			pre = s[i+1:]
			s = s[:i]
			break
		}
	}
	var parts []int
	for _, f := range strings.Split(s, ".") {
		n := 0
		for _, r := range f {
			if r < '0' || r > '9' {
				break
			}
			n = n*10 + int(r-'0')
		}
		parts = append(parts, n)
	}
	return Semver{parts: parts, pre: pre}
}

// Compare returns -1, 0, or 1 when a is less than, equal to, or greater than b.
func (a Semver) Compare(b Semver) int {
	n := len(a.parts)
	if len(b.parts) > n {
		n = len(b.parts)
	}
	for i := 0; i < n; i++ {
		var av, bv int
		if i < len(a.parts) {
			av = a.parts[i]
		}
		if i < len(b.parts) {
			bv = b.parts[i]
		}
		if av != bv {
			if av < bv {
				return -1
			}
			return 1
		}
	}
	// Equal numeric parts: a release outranks a pre-release.
	switch {
	case a.pre == "" && b.pre != "":
		return 1
	case a.pre != "" && b.pre == "":
		return -1
	case a.pre < b.pre:
		return -1
	case a.pre > b.pre:
		return 1
	}
	return 0
}

// AtLeast reports whether version v satisfies the minimum min (v >= min).
func AtLeast(v, min string) bool {
	return ParseSemver(v).Compare(ParseSemver(min)) >= 0
}

// String renders the build info for human output.
func (i Info) String() string {
	return fmt.Sprintf("leptostack %s", i.Version)
}
