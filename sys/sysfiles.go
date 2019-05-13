package sys

import (
	"fmt"
	"os"
)

type Unixfile struct {
	os.FileInfo
	User  string
	Group string
	Path  string
}

func (f Unixfile) PaddedName(l int) string {
	return fmt.Sprintf("%-"+fmt.Sprintf("%d", l)+"s", f.Name())
}

func (f Unixfile) PaddedSize(l int) string {
	return fmt.Sprintf("%-"+fmt.Sprintf("%d", l)+"d", f.Size())
}

func (f Unixfile) PaddedUser(l int) string {
	return fmt.Sprintf("%-"+fmt.Sprintf("%d", l)+"s", f.User)
}

func (f Unixfile) PaddedGroup(l int) string {
	return fmt.Sprintf("%-"+fmt.Sprintf("%d", l)+"s", f.Group)
}

