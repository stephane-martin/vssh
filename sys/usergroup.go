package sys

import (
	"fmt"
	"os"
	"os/user"
	"syscall"

	"github.com/pkg/sftp"
)

func UserGroupNum(info os.FileInfo) (int, int) {
	if i, ok := info.Sys().(*sftp.FileStat); ok {
		return int(i.UID), int(i.GID)
	}
	if i, ok := info.Sys().(*syscall.Stat_t); ok {
		return int(i.Uid), int(i.Gid)
	}
	return -1, -1
}

func UserGroup(info os.FileInfo, remote bool) (string, string) {
	uid, gid := UserGroupNum(info)
	if uid == -1 && gid == -1 {
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
