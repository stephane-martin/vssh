package lib

import (
	"fmt"
	"os"
	"os/user"
	"syscall"
)

func UserGroup(info os.FileInfo) (string, string) {
	if i, ok := info.Sys().(*syscall.Stat_t); ok {
		u, err := user.LookupId(fmt.Sprintf("%d", i.Uid))
		if err != nil {
			return "", ""
		}
		g, err := user.LookupGroupId(fmt.Sprintf("%d", i.Gid))
		if err != nil {
			return "", ""
		}
		return u.Username, g.Name
	}
	return "", ""
}
