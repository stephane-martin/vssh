package sys

import (
	"fmt"
	"os"
)

type UFile struct {
	os.FileInfo
	User  string
	Group string
	Path  string
}

func (f UFile) FSize() string {
	size := f.Size()
	if size < 1024 {
		return fmt.Sprintf("%d", size)
	}
	ksize := float64(size) / 1024
	if ksize < 1024 {
		return fmt.Sprintf("%.1fK", ksize)
	}
	msize := ksize / 1024
	if msize < 1024 {
		return fmt.Sprintf("%.1fM", msize)
	}
	return fmt.Sprintf("%.1fG", msize/1024)
}

func (f UFile) PaddedName(l int) string {
	return fmt.Sprintf("%-"+fmt.Sprintf("%d", l)+"s", f.Name())
}

func (f UFile) PaddedSize(l int) string {
	if !f.Mode().IsRegular() {
		return fmt.Sprintf("%-"+fmt.Sprintf("%d", l)+"s", "-")
	}
	return fmt.Sprintf("%-"+fmt.Sprintf("%d", l)+"s", f.FSize())
}

func (f UFile) PaddedUser(l int) string {
	return fmt.Sprintf("%-"+fmt.Sprintf("%d", l)+"s", f.User)
}

func (f UFile) PaddedGroup(l int) string {
	return fmt.Sprintf("%-"+fmt.Sprintf("%d", l)+"s", f.Group)
}
