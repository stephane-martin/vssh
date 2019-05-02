package lib

import (
	"fmt"
	"os"
	"sort"

	"github.com/ahmetb/go-linq"
)

type SelectedAction uint8

type SelectedCallback func(*SelectedFile) ([]os.FileInfo, error)

// TODO: download, upload, delete, edit, help

const (
	Init SelectedAction = iota
	ViewFile
	OpenDir
	OpenFile
	DeleteFile
	Refresh
	Stop
)

type SelectedFile struct {
	Name     string
	Size     int64
	Mode     os.FileMode
	Position int
	Action   SelectedAction
	IsDir    bool
	Err      error
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

type Unixfiles struct {
	AllFiles   []Unixfile
	Dirs       []Unixfile
	Regulars   []Unixfile
	Irregulars []Unixfile
}

func (files *Unixfiles) maxNameLength() int {
	if len(files.AllFiles) == 0 {
		return 0
	}
	return linq.From(files.AllFiles).SelectT(func(file Unixfile) int { return len(file.Name()) }).Max().(int)
}

func (files *Unixfiles) maxSizeLength() int {
	if len(files.AllFiles) == 0 {
		return 0
	}

	return linq.From(files.AllFiles).SelectT(func(file Unixfile) int { return len(fmt.Sprintf("%d", file.Size())) }).Max().(int)
}

func (files *Unixfiles) maxUserLength() int {
	if len(files.AllFiles) == 0 {
		return 0
	}

	return linq.From(files.AllFiles).SelectT(func(file Unixfile) int { return len(file.User) }).Max().(int)
}

func (files *Unixfiles) maxGroupLength() int {
	if len(files.AllFiles) == 0 {
		return 0
	}

	return linq.From(files.AllFiles).SelectT(func(file Unixfile) int { return len(file.Group) }).Max().(int)
}

func (files *Unixfiles) getSelectedFile(position int) *SelectedFile {
	if position == 0 || (position >= (len(files.AllFiles) + 2)) {
		return nil
	}
	if position == 1 {
		return &SelectedFile{Name: "..", Position: position, IsDir: true}
	}
	f := files.AllFiles[position-2]
	if f.IsDir() || f.Mode().IsRegular() {
		return &SelectedFile{Name: f.Name(), Size: f.Size(), Mode: f.Mode(), Position: position, IsDir: f.IsDir()}
	}
	return nil
}

func (all *Unixfiles) Init(files []os.FileInfo, remote bool) *Unixfiles {
	all.Dirs = make([]Unixfile, 0, len(files))
	all.Regulars = make([]Unixfile, 0, len(files))
	all.Irregulars = make([]Unixfile, 0, len(files))
	for _, f := range files {
		u, g := UserGroup(f, remote)
		uf := Unixfile{FileInfo: f, User: u, Group: g}
		if f.IsDir() {
			all.Dirs = append(all.Dirs, uf)
		} else if f.Mode().IsRegular() {
			all.Regulars = append(all.Regulars, uf)
		} else {
			all.Irregulars = append(all.Irregulars, uf)
		}
	}
	sort.Slice(all.Dirs, func(i, j int) bool { return all.Dirs[i].Name() < all.Dirs[j].Name() })
	sort.Slice(all.Regulars, func(i, j int) bool { return all.Regulars[i].Name() < all.Regulars[j].Name() })
	sort.Slice(all.Irregulars, func(i, j int) bool { return all.Irregulars[i].Name() < all.Irregulars[j].Name() })

	all.AllFiles = make([]Unixfile, 0, len(all.Dirs)+len(all.Regulars)+len(all.Irregulars))
	all.AllFiles = append(all.AllFiles, all.Dirs...)
	all.AllFiles = append(all.AllFiles, all.Irregulars...)
	all.AllFiles = append(all.AllFiles, all.Regulars...)
	return all
}
