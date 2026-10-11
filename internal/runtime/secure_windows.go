//go:build windows

package runtime

import "os"

func ownedByUID(info os.FileInfo, uid int) bool { return true }
