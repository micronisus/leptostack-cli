//go:build windows

package upgrade

import (
	"fmt"
	"os"
)

// replaceExecutable swaps the running binary on Windows, where an open
// executable cannot be overwritten or removed. The running binary is renamed
// aside to self+".old" (best-effort cleanup of any previous one first), then the
// new binary is moved into place. The stale ".old" is removed by CleanupOld on
// the next start; if antivirus or file locking still blocks the move, the error
// points the user at a manual download.
func replaceExecutable(tmp, self string) error {
	old := self + ".old"
	_ = os.Remove(old)
	if err := os.Rename(self, old); err != nil {
		return fmt.Errorf("moving the running binary aside failed; close leptostack and download %s manually from the releases page: %w", self, err)
	}
	if err := os.Rename(tmp, self); err != nil {
		_ = os.Rename(old, self) // restore the original on failure
		_ = os.Remove(tmp)
		return fmt.Errorf("installing the new binary failed; download %s manually from the releases page: %w", self, err)
	}
	return nil
}
