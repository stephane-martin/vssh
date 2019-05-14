package widgets

import (
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/stephane-martin/vssh/sys"

	"github.com/ahmetb/go-linq"
	"github.com/gdamore/tcell"
	"github.com/rivo/tview"
)

var ErrSwitch = errors.New("switch")

type tableOfFiles struct {
	app      *tview.Application
	pages    *tview.Pages
	table    *tview.Table
	files    *Unixfiles
	err      error
	errlock  sync.Mutex
	callback SelectedCallback
	readFile func(string) ([]byte, error)
	remote   bool
}

func (table *tableOfFiles) setErr(err error) {
	if err == nil {
		return
	}
	table.errlock.Lock()
	if table.err == nil {
		table.err = err
	}
	table.errlock.Unlock()
}

func (table *tableOfFiles) fill() {

	maxNameLength := table.files.maxNameLength()
	maxSizeLength := table.files.maxSizeLength()
	maxUserLength := table.files.maxUserLength()
	maxGroupLength := table.files.maxGroupLength()

	abort := func(e error) {
		table.setErr(e)
		table.app.Stop()
	}

	table.table.Clear()

	table.table.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		key := event.Key()
		r := event.Rune()
		if r == 'q' || key == tcell.KeyEscape {
			abort(nil)
			return nil
		}
		if key == tcell.KeyEnter {
			position, _ := table.table.GetSelection()
			f := table.files.getSelectedFile(position)
			if f == nil {
				return nil
			}
			if f.IsDir {
				f.Action = OpenDir
				files, err := table.callback(f)
				if err != nil {
					abort(err)
					return nil
				}
				table.files.Init(files, table.remote)
				table.fill()
				table.table.Select(1, 0)
				return nil
			}
			if f.Mode.IsRegular() {
				content, err := table.readFile(f.Name)
				if err != nil {
					abort(err)
					return nil
				}
				w, err := ShowFileInternalWidget(f.Name, content)
				if err != nil {
					abort(err)
					return nil
				}
				w.SetInputCapture(func(ev *tcell.EventKey) *tcell.EventKey {
					key := ev.Key()
					if key == tcell.KeyEnter {
						return tcell.NewEventKey(tcell.KeyDown, 'j', tcell.ModNone)
					}
					if key == tcell.KeyESC || ev.Rune() == 'q' {
						table.pages.RemovePage("less")
					}
					return ev
				})
				table.pages.AddPage("less", w, true, true)
			}
			return nil
		}
		if r == 's' {
			abort(ErrSwitch)
			return nil
		}
		if r == 'r' {
			files, err := table.callback(nil)
			if err != nil {
				abort(err)
				return nil
			}
			table.files.Init(files, table.remote)
			table.fill()
			table.table.Select(1, 0)
			return nil
		}
		if r == 'e' {
			position, _ := table.table.GetSelection()
			f := table.files.getSelectedFile(position)
			if f == nil {
				return nil
			}
			if f.IsDir {
				return nil
			}
			if f.Mode.IsRegular() {
				f.Action = EditFile
				var files []os.FileInfo
				var err error
				table.app.Suspend(func() {
					files, err = table.callback(f)
				})
				if err != nil {
					abort(err)
					return nil
				}
				table.files.Init(files, table.remote)
				table.fill()
				table.table.Select(1, 0)
			}
			return nil
		}

		if r == 'o' {
			position, _ := table.table.GetSelection()
			f := table.files.getSelectedFile(position)
			if f == nil {
				return nil
			}
			if f.IsDir {
				f.Action = OpenDir
				files, err := table.callback(f)
				if err != nil {
					abort(err)
					return nil
				}
				table.files.Init(files, table.remote)
				table.fill()
				table.table.Select(1, 0)
				return nil
			}
			if f.Mode.IsRegular() {
				f.Action = OpenFile
				_, err := table.callback(f)
				if err != nil {
					abort(err)
					return nil
				}
			}
			return nil
		}
		if r == 'D' {
			position, _ := table.table.GetSelection()
			f := table.files.getSelectedFile(position)
			if f == nil {
				return nil
			}
			if f.IsDir {
				if f.Name == ".." {
					return nil
				}
				modal := tview.NewModal().
					SetText(fmt.Sprintf("Do you want to delete the directory?\n[blue]%s[-]", f.Name)).
					AddButtons([]string{"Delete", "Cancel"}).
					SetDoneFunc(func(buttonIndex int, buttonLabel string) {
						table.pages.RemovePage("confirmDelete")
						if buttonLabel == "Delete" {
							f.Action = DeleteDir
							files, err := table.callback(f)
							if err != nil {
								abort(err)
								return
							}
							table.files.Init(files, table.remote)
							table.fill()
							table.table.Select(1, 0)
						}
					})
				table.pages.AddPage("confirmDelete", modal, true, true)
				return nil
			}
			if f.Mode.IsRegular() {
				modal := tview.NewModal().
					SetText(fmt.Sprintf("Do you want to delete the file?\n[blue]%s[-]", f.Name)).
					AddButtons([]string{"Delete", "Cancel"}).
					SetDoneFunc(func(buttonIndex int, buttonLabel string) {
						table.pages.RemovePage("confirmDelete")
						if buttonLabel == "Delete" {
							f.Action = DeleteFile
							files, err := table.callback(f)
							if err != nil {
								abort(err)
								return
							}
							table.files.Init(files, table.remote)
							table.fill()
							table.table.Select(1, 0)
						}
					})
				table.pages.AddPage("confirmDelete", modal, true, true)
			}
			return nil
		}
		return event
	})

	var bold tcell.Style
	bold = bold.Bold(true)
	var row int
	table.table.SetCell(row, 0, tview.NewTableCell("Name").SetTextColor(tcell.ColorDarkGray).SetStyle(bold).SetExpansion(4))
	table.table.SetCell(row, 1, tview.NewTableCell("Size").SetTextColor(tcell.ColorDarkGray).SetStyle(bold).SetExpansion(1))
	table.table.SetCell(row, 2, tview.NewTableCell("User").SetTextColor(tcell.ColorDarkGray).SetStyle(bold).SetExpansion(1))
	table.table.SetCell(row, 3, tview.NewTableCell("Group").SetTextColor(tcell.ColorDarkGray).SetStyle(bold).SetExpansion(1))
	table.table.SetCell(row, 4, tview.NewTableCell("Perms").SetTextColor(tcell.ColorDarkGray).SetStyle(bold).SetExpansion(1))
	table.table.SetCell(row, 5, tview.NewTableCell("Mod").SetTextColor(tcell.ColorDarkGray).SetStyle(bold).SetExpansion(1))
	row++
	table.table.SetCell(row, 0, tview.NewTableCell("..").SetTextColor(tcell.ColorLightBlue))
	row++
	for _, d := range table.files.Dirs {
		c := tview.NewTableCell(d.PaddedName(maxNameLength))
		if strings.HasPrefix(d.Name(), ".") {
			c.SetTextColor(tcell.ColorLightBlue)
		} else {
			c.SetStyle(bold).SetTextColor(tcell.ColorLightBlue)
		}
		table.table.SetCell(row, 0, c)
		table.table.SetCell(row, 1, tview.NewTableCell(d.PaddedSize(maxSizeLength)))
		table.table.SetCell(row, 2, tview.NewTableCell(d.PaddedUser(maxUserLength)).SetTextColor(tcell.ColorKhaki))
		table.table.SetCell(row, 3, tview.NewTableCell(d.PaddedGroup(maxGroupLength)).SetTextColor(tcell.ColorKhaki))
		table.table.SetCell(row, 4, tview.NewTableCell(fmt.Sprintf("%-12s", d.Mode().Perm())).SetTextColor(tcell.ColorLightCoral))
		table.table.SetCell(row, 5, tview.NewTableCell(d.ModTime().Format(time.RFC822)).SetTextColor(tcell.ColorLightSeaGreen))
		row++
	}
	for _, irr := range table.files.Irregulars {
		c := tview.NewTableCell(irr.PaddedName(maxNameLength)).SetTextColor(tcell.ColorViolet)
		table.table.SetCell(row, 0, c)
		table.table.SetCell(row, 1, tview.NewTableCell(irr.PaddedSize(maxSizeLength)))
		table.table.SetCell(row, 2, tview.NewTableCell(irr.PaddedUser(maxUserLength)).SetTextColor(tcell.ColorKhaki))
		table.table.SetCell(row, 3, tview.NewTableCell(irr.PaddedGroup(maxGroupLength)).SetTextColor(tcell.ColorKhaki))
		table.table.SetCell(row, 4, tview.NewTableCell(fmt.Sprintf("%-12s", irr.Mode().Perm())).SetTextColor(tcell.ColorLightCoral))
		table.table.SetCell(row, 5, tview.NewTableCell(irr.ModTime().Format(time.RFC822)).SetTextColor(tcell.ColorLightSeaGreen))
		row++
	}
	for _, reg := range table.files.Regulars {
		c := tview.NewTableCell(reg.PaddedName(maxNameLength))
		color := tcell.ColorDefault
		if (reg.Mode().Perm() & 0100) != 0 {
			color = tcell.ColorGreen
		}
		if strings.HasPrefix(reg.Name(), ".") {
			c.SetTextColor(color)
		} else {
			c.SetStyle(bold).SetTextColor(color)
		}
		table.table.SetCell(row, 0, c)
		table.table.SetCell(row, 1, tview.NewTableCell(reg.PaddedSize(maxSizeLength)))
		table.table.SetCell(row, 2, tview.NewTableCell(reg.PaddedUser(maxUserLength)).SetTextColor(tcell.ColorKhaki))
		table.table.SetCell(row, 3, tview.NewTableCell(reg.PaddedGroup(maxGroupLength)).SetTextColor(tcell.ColorKhaki))
		table.table.SetCell(row, 4, tview.NewTableCell(fmt.Sprintf("%-12s", reg.Mode().Perm())).SetTextColor(tcell.ColorLightCoral))
		table.table.SetCell(row, 5, tview.NewTableCell(reg.ModTime().Format(time.RFC822)).SetTextColor(tcell.ColorLightSeaGreen))
		row++
	}

}

func TableOfFiles(wd string, callback SelectedCallback, readFile func(string) ([]byte, error), remote bool) error {
	// TODO: formatting modtime
	table := new(tableOfFiles)
	table.callback = callback
	table.readFile = readFile
	table.remote = remote
	files, err := callback(nil)
	if err != nil {
		return err
	}
	table.files = new(Unixfiles)
	table.files.Init(files, remote)

	table.app = tview.NewApplication()
	// TODO: table.app.SetInputCapture()
	title := fmt.Sprintf(" [violet]%s[-] (%%s) ", wd)
	if remote {
		title = fmt.Sprintf(title, "remote")
	} else {
		title = fmt.Sprintf(title, "local")
	}
	table.table = tview.NewTable().SetBorders(false).SetFixed(1, 0)
	table.pages = tview.NewPages()
	table.pages.AddPage("table", table.table, true, true)
	table.table.SetSelectable(true, false)
	table.table.SetSelectedStyle(tcell.ColorRed, tcell.ColorDefault, tcell.AttrBold)
	table.table.SetBorder(true).SetBorderPadding(1, 0, 1, 1)
	table.table.SetTitle(title)
	table.fill()
	table.table.Select(1, 0)

	err = table.app.SetRoot(table.pages, true).Run()
	if err != nil {
		return err
	}
	return table.err
}

type Unixfiles struct {
	AllFiles   []sys.Unixfile
	Dirs       []sys.Unixfile
	Regulars   []sys.Unixfile
	Irregulars []sys.Unixfile
}

func (files *Unixfiles) maxNameLength() int {
	if len(files.AllFiles) == 0 {
		return 0
	}
	return linq.From(files.AllFiles).SelectT(func(file sys.Unixfile) int { return len(file.Name()) }).Max().(int)
}

func (files *Unixfiles) maxSizeLength() int {
	if len(files.AllFiles) == 0 {
		return 0
	}

	return linq.From(files.AllFiles).SelectT(func(file sys.Unixfile) int { return len(fmt.Sprintf("%d", file.Size())) }).Max().(int)
}

func (files *Unixfiles) maxUserLength() int {
	if len(files.AllFiles) == 0 {
		return 0
	}

	return linq.From(files.AllFiles).SelectT(func(file sys.Unixfile) int { return len(file.User) }).Max().(int)
}

func (files *Unixfiles) maxGroupLength() int {
	if len(files.AllFiles) == 0 {
		return 0
	}

	return linq.From(files.AllFiles).SelectT(func(file sys.Unixfile) int { return len(file.Group) }).Max().(int)
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
	all.Dirs = make([]sys.Unixfile, 0, len(files))
	all.Regulars = make([]sys.Unixfile, 0, len(files))
	all.Irregulars = make([]sys.Unixfile, 0, len(files))
	for _, f := range files {
		u, g := sys.UserGroup(f, remote)
		uf := sys.Unixfile{FileInfo: f, User: u, Group: g}
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

	all.AllFiles = make([]sys.Unixfile, 0, len(all.Dirs)+len(all.Regulars)+len(all.Irregulars))
	all.AllFiles = append(all.AllFiles, all.Dirs...)
	all.AllFiles = append(all.AllFiles, all.Irregulars...)
	all.AllFiles = append(all.AllFiles, all.Regulars...)
	return all
}
