package lib

import (
	"fmt"
	"os"

	"github.com/ahmetb/go-linq"
)

type SelectedFile struct {
	Name string
	Size int64
	Mode os.FileMode
}

type Unixfile struct {
	os.FileInfo
	User  string
	Group string
	Path  string
}

func (f Unixfile) paddedName(l int) string {
	return fmt.Sprintf("%-"+fmt.Sprintf("%d", l)+"s", f.Name())
}

func (f Unixfile) paddedSize(l int) string {
	return fmt.Sprintf("%-"+fmt.Sprintf("%d", l)+"d", f.Size())
}

func (f Unixfile) paddedUser(l int) string {
	return fmt.Sprintf("%-"+fmt.Sprintf("%d", l)+"s", f.User)
}

func (f Unixfile) paddedGroup(l int) string {
	return fmt.Sprintf("%-"+fmt.Sprintf("%d", l)+"s", f.Group)
}

type Unixfiles []Unixfile

func (files Unixfiles) maxNameLength() int {
	if len(files) == 0 {
		return 0
	}
	return linq.From(files).SelectT(func(file Unixfile) int { return len(file.Name()) }).Max().(int)
}

func (files Unixfiles) maxSizeLength() int {
	if len(files) == 0 {
		return 0
	}

	return linq.From(files).SelectT(func(file Unixfile) int { return len(fmt.Sprintf("%d", file.Size())) }).Max().(int)
}

func (files Unixfiles) maxUserLength() int {
	if len(files) == 0 {
		return 0
	}

	return linq.From(files).SelectT(func(file Unixfile) int { return len(file.User) }).Max().(int)
}

func (files Unixfiles) maxGroupLength() int {
	if len(files) == 0 {
		return 0
	}

	return linq.From(files).SelectT(func(file Unixfile) int { return len(file.Group) }).Max().(int)
}
