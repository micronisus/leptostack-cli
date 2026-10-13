//go:build !windows

package upgrade

import "os"

// replaceExecutable atomically replaces the running binary. On Unix an open
// executable can be renamed over, so this is a plain rename.
func replaceExecutable(tmp, self string) error {
	if err := os.Rename(tmp, self); err != nil {
		return err
	}
	return nil
}
