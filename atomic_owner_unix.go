//go:build unix

package main

import (
	"os"
	"syscall"
)

// keepOwner gives the replacement file the owner and group of the file it
// replaces, for example a guide shared with Tvheadend through its group.
// Without permission to keep the owner, the group is kept if possible.
func keepOwner(f *os.File, info os.FileInfo) {
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return
	}
	if f.Chown(int(st.Uid), int(st.Gid)) != nil {
		_ = f.Chown(-1, int(st.Gid))
	}
}
