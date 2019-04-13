package lib

import (
	"fmt"
	"os"
	"os/user"
	"syscall"

	"github.com/pkg/sftp"
)

func UserGroup(info os.FileInfo, remote bool) (string, string) {
	var uid, gid uint32
	if i, ok := info.Sys().(*sftp.FileStat); ok {
		uid = i.UID
		gid = i.GID
	} else if i, ok := info.Sys().(*syscall.Stat_t); ok {
		uid = i.Uid
		gid = i.Gid
	} else {
		return "", ""
	}
	if remote {
		return fmt.Sprintf("%d", uid), fmt.Sprintf("%d", gid)
	}
	u, err := user.LookupId(fmt.Sprintf("%d", uid))
	if err != nil {
		return fmt.Sprintf("%d", uid), fmt.Sprintf("%d", gid)
	}
	g, err := user.LookupGroupId(fmt.Sprintf("%d", gid))
	if err != nil {
		return fmt.Sprintf("%d", uid), fmt.Sprintf("%d", gid)
	}
	return u.Username, g.Name
}
