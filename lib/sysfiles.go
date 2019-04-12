package lib

import (
	"fmt"
	"io"
	"os"
	"time"

	"github.com/ahmetb/go-linq"
)

type Stater interface {
	io.Reader
	Stat() (os.FileInfo, error)
}

type ReaderFileStater struct {
	io.Reader
	Name string
}

func (f ReaderFileStater) Stat() (os.FileInfo, error) {
	return fileStat{name: f.Name}, nil
}

type fileStat struct {
	name string
}

func (fs fileStat) Size() int64        { return 0 }
func (fs fileStat) Mode() os.FileMode  { return 0 }
func (fs fileStat) ModTime() time.Time { return time.Now() }
func (fs fileStat) Sys() interface{}   { return nil }
func (fs fileStat) Name() string       { return fs.name }
func (fs fileStat) IsDir() bool        { return false }

type SelectedFile struct {
	Name string
	Size int64
	Mode os.FileMode
}

type Unixfile struct {
	os.FileInfo
	User  string
	Group string
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
	return linq.From(files).SelectT(func(file Unixfile) int { return len(file.Name()) }).Max().(int)
}

func (files Unixfiles) maxSizeLength() int {
	return linq.From(files).SelectT(func(file Unixfile) int { return len(fmt.Sprintf("%d", file.Size())) }).Max().(int)
}

func (files Unixfiles) maxUserLength() int {
	return linq.From(files).SelectT(func(file Unixfile) int { return len(file.User) }).Max().(int)
}

func (files Unixfiles) maxGroupLength() int {
	return linq.From(files).SelectT(func(file Unixfile) int { return len(file.Group) }).Max().(int)
}
