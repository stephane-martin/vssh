package widgets

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
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
	files    *UFiles
	err      error
	errlock  sync.Mutex
	callback SelectedCallback
	readFile func(string) ([]byte, error)
	remote   bool
	wd       string
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

func (table *tableOfFiles) chdir(f *SelectedFile) {
	f.Action = OpenDir
	files, err := table.callback(f)
	if err != nil {
		table.abort(err)
		return
	}
	table.wd = filepath.Clean(filepath.Join(table.wd, f.Name))
	table.refresh(files)
}

func (table *tableOfFiles) abort(err error) {
	table.setErr(err)
	table.app.Stop()
}

func (table *tableOfFiles) less(f *SelectedFile) {
	content, err := table.readFile(f.Name)
	if err != nil {
		table.abort(err)
		return
	}
	w, err := ShowFileInternalWidget(f.Name, content)
	if err != nil {
		table.abort(err)
		return
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

func (table *tableOfFiles) edit(f *SelectedFile) {
	f.Action = EditFile
	var files []os.FileInfo
	var err error
	table.app.Suspend(func() {
		files, err = table.callback(f)
	})
	if err != nil {
		table.abort(err)
		return
	}
	table.refresh(files)
}

func (table *tableOfFiles) open(f *SelectedFile) {
	f.Action = OpenFile
	_, err := table.callback(f)
	if err != nil {
		table.abort(err)
	}
}

func (table *tableOfFiles) rmdir(f *SelectedFile) {
	modal := tview.NewModal().
		SetText(fmt.Sprintf("Do you want to delete the directory?\n[blue]%s[-]", f.Name)).
		AddButtons([]string{"Delete", "Cancel"}).
		SetDoneFunc(func(buttonIndex int, buttonLabel string) {
			table.pages.RemovePage("confirmDelete")
			if buttonLabel == "Delete" {
				f.Action = DeleteDir
				files, err := table.callback(f)
				if err != nil {
					table.abort(err)
					return
				}
				table.refresh(files)
			}
		})
	table.pages.AddPage("confirmDelete", modal, true, true)
}

func (table *tableOfFiles) rmfile(f *SelectedFile) {
	modal := tview.NewModal().
		SetText(fmt.Sprintf("Do you want to delete the file?\n[blue]%s[-]", f.Name)).
		AddButtons([]string{"Delete", "Cancel"}).
		SetDoneFunc(func(buttonIndex int, buttonLabel string) {
			table.pages.RemovePage("confirmDelete")
			if buttonLabel == "Delete" {
				f.Action = DeleteFile
				files, err := table.callback(f)
				if err != nil {
					table.abort(err)
					return
				}
				table.refresh(files)
			}
		})
	table.pages.AddPage("confirmDelete", modal, true, true)
}

func (table *tableOfFiles) refresh(files []os.FileInfo) {
	table.app.QueueUpdateDraw(func() {
		table.files.Init(files, table.remote)
		table.fill()
		table.table.Select(1, 0)
		table.table.SetOffset(0, 0)
	})
}

func (table *tableOfFiles) fill() {

	maxNameLength := table.files.maxNameLength()
	maxSizeLength := table.files.maxSizeLength()
	maxUserLength := table.files.maxUserLength()
	maxGroupLength := table.files.maxGroupLength()

	table.table.Clear()

	title := fmt.Sprintf(" [violet]%s[-] (%%s) ", table.wd)
	if table.remote {
		title = fmt.Sprintf(title, "remote")
	} else {
		title = fmt.Sprintf(title, "local")
	}
	table.table.SetTitle(title)

	table.table.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		key := event.Key()
		r := event.Rune()
		if r == 'q' || key == tcell.KeyEscape {
			table.abort(nil)
			return nil
		}
		if key == tcell.KeyEnter {
			position, _ := table.table.GetSelection()
			f := table.files.getSelectedFile(position)
			if f == nil {
				return nil
			}
			if f.IsDir {
				table.chdir(f)
				return nil
			}
			if f.Mode.IsRegular() {
				table.less(f)
			}
			return nil
		}
		if r == 's' {
			table.abort(ErrSwitch)
			return nil
		}
		if r == 'r' {
			files, err := table.callback(nil)
			if err != nil {
				table.abort(err)
				return nil
			}
			table.refresh(files)
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
				table.edit(f)
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
				table.chdir(f)
				return nil
			}
			if f.Mode.IsRegular() {
				table.open(f)
			}
			return nil
		}
		if r == 'D' {
			position, _ := table.table.GetSelection()
			f := table.files.getSelectedFile(position)
			if f == nil {
				return nil
			}
			if f.Name == ".." {
				return nil
			}
			if f.IsDir {
				table.rmdir(f)
				return nil
			}
			if f.Mode.IsRegular() {
				table.rmfile(f)
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
	table.wd = wd
	files, err := callback(nil)
	if err != nil {
		return err
	}
	table.files = new(UFiles)
	table.files.Init(files, remote)

	table.app = tview.NewApplication()
	table.table = tview.NewTable().SetBorders(false).SetFixed(1, 0)
	table.pages = tview.NewPages()
	table.pages.AddPage("table", table.table, true, true)
	table.table.SetSelectable(true, false)
	table.table.SetSelectedStyle(tcell.ColorRed, tcell.ColorDefault, tcell.AttrBold)
	table.table.SetBorder(true).SetBorderPadding(1, 0, 1, 1)
	table.fill()
	table.table.Select(1, 0)

	err = table.app.SetRoot(table.pages, true).Run()
	if err != nil {
		return err
	}
	return table.err
}

type UFiles struct {
	AllFiles   []sys.UFile
	Dirs       []sys.UFile
	Regulars   []sys.UFile
	Irregulars []sys.UFile
}

func (all *UFiles) maxNameLength() int {
	if len(all.AllFiles) == 0 {
		return 0
	}
	return linq.From(all.AllFiles).SelectT(func(file sys.UFile) int { return len(file.Name()) }).Max().(int)
}

func (all *UFiles) maxSizeLength() int {
	if len(all.AllFiles) == 0 {
		return 0
	}

	return linq.From(all.AllFiles).SelectT(func(file sys.UFile) int { return len(file.FSize()) }).Max().(int)
}

func (all *UFiles) maxUserLength() int {
	if len(all.AllFiles) == 0 {
		return 0
	}

	return linq.From(all.AllFiles).SelectT(func(file sys.UFile) int { return len(file.User) }).Max().(int)
}

func (all *UFiles) maxGroupLength() int {
	if len(all.AllFiles) == 0 {
		return 0
	}

	return linq.From(all.AllFiles).SelectT(func(file sys.UFile) int { return len(file.Group) }).Max().(int)
}

func (all *UFiles) getSelectedFile(position int) *SelectedFile {
	if position == 0 || (position >= (len(all.AllFiles) + 2)) {
		return nil
	}
	if position == 1 {
		return &SelectedFile{Name: "..", Position: position, IsDir: true}
	}
	f := all.AllFiles[position-2]
	if f.IsDir() || f.Mode().IsRegular() {
		return &SelectedFile{Name: f.Name(), Size: f.Size(), Mode: f.Mode(), Position: position, IsDir: f.IsDir()}
	}
	return nil
}

func (all *UFiles) Init(files []os.FileInfo, remote bool) *UFiles {
	all.Dirs = make([]sys.UFile, 0, len(files))
	all.Regulars = make([]sys.UFile, 0, len(files))
	all.Irregulars = make([]sys.UFile, 0, len(files))
	for _, f := range files {
		u, g := sys.UserGroup(f, remote)
		uf := sys.UFile{FileInfo: f, User: u, Group: g}
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

	all.AllFiles = make([]sys.UFile, 0, len(all.Dirs)+len(all.Regulars)+len(all.Irregulars))
	all.AllFiles = append(all.AllFiles, all.Dirs...)
	all.AllFiles = append(all.AllFiles, all.Irregulars...)
	all.AllFiles = append(all.AllFiles, all.Regulars...)
	return all
}
