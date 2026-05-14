//go:build !windows

package filemanager

import (
	"io/fs"
	"os/user"
	"strconv"
	"syscall"
)

func populateOwnerGroup(info fs.FileInfo, fi *FileInfo) {
	if stat, ok := info.Sys().(*syscall.Stat_t); ok {
		if u, err := user.LookupId(strconv.Itoa(int(stat.Uid))); err == nil {
			fi.User = u.Username
		} else {
			fi.User = strconv.Itoa(int(stat.Uid))
		}
		if g, err := user.LookupGroupId(strconv.Itoa(int(stat.Gid))); err == nil {
			fi.Group = g.Name
		} else {
			fi.Group = strconv.Itoa(int(stat.Gid))
		}
	}
}
