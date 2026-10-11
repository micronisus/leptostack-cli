//go:build unix

package runtime

import (
	"os"
	"syscall"
)

func ownedByUID(info os.FileInfo, uid int) bool {
	if st, ok := info.Sys().(*syscall.Stat_t); ok {
		return int(st.Uid) == uid
	}
	return true
}
