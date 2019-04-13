package lib

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sort"
	"strings"
	"sync/atomic"
	"time"

	"github.com/ahmetb/go-linq"
	"github.com/gdamore/tcell"
	"github.com/logrusorgru/aurora"
	"github.com/rivo/tview"
)

func TableOfFiles(dirname string, files []os.FileInfo, remote bool) (SelectedFile, error) {
	var selected atomic.Value
	selected.Store(SelectedFile{})
	var dirs, regulars, irregulars Unixfiles
	for _, f := range files {
		u, g := UserGroup(f, remote)
		uf := Unixfile{FileInfo: f, User: u, Group: g}
		if f.IsDir() {
			dirs = append(dirs, uf)
		} else if f.Mode().IsRegular() {
			regulars = append(regulars, uf)
		} else {
			irregulars = append(irregulars, uf)
		}
	}
	sort.Slice(dirs, func(i, j int) bool { return dirs[i].Name() < dirs[j].Name() })
	sort.Slice(regulars, func(i, j int) bool { return regulars[i].Name() < regulars[j].Name() })
	sort.Slice(irregulars, func(i, j int) bool { return irregulars[i].Name() < regulars[j].Name() })

	var allfiles Unixfiles = make([]Unixfile, 0, len(dirs)+len(regulars)+len(irregulars))
	allfiles = append(allfiles, dirs...)
	allfiles = append(allfiles, irregulars...)
	allfiles = append(allfiles, regulars...)

	maxNameLength := allfiles.maxNameLength()
	maxSizeLength := allfiles.maxSizeLength()
	maxUserLength := allfiles.maxUserLength()
	maxGroupLength := allfiles.maxGroupLength()

	app := tview.NewApplication()
	app.SetInputCapture(func(ev *tcell.EventKey) *tcell.EventKey {
		if ev.Key() == tcell.KeyCtrlC {
			app.Stop()
			return nil
		}
		return ev
	})

	table := tview.NewTable().SetBorders(false).SetFixed(1, 0)
	table.
		SetSelectable(true, false).
		SetSelectedStyle(tcell.ColorRed, tcell.ColorDefault, tcell.AttrBold)
	table.SetBorder(true).SetBorderPadding(1, 0, 1, 1).SetTitle(" " + dirname + " ")

	table.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		key := event.Key()
		if event.Rune() == 'q' || key == tcell.KeyEscape {
			app.Stop()
			return nil
		}
		return event
	})

	var bold tcell.Style
	bold = bold.Bold(true)
	var row int
	table.SetCell(row, 0, tview.NewTableCell("Name").SetTextColor(tcell.ColorDarkGray).SetStyle(bold).SetExpansion(4))
	table.SetCell(row, 1, tview.NewTableCell("Size").SetTextColor(tcell.ColorDarkGray).SetStyle(bold).SetExpansion(1))
	table.SetCell(row, 2, tview.NewTableCell("User").SetTextColor(tcell.ColorDarkGray).SetStyle(bold).SetExpansion(1))
	table.SetCell(row, 3, tview.NewTableCell("Group").SetTextColor(tcell.ColorDarkGray).SetStyle(bold).SetExpansion(1))
	table.SetCell(row, 4, tview.NewTableCell("Perms").SetTextColor(tcell.ColorDarkGray).SetStyle(bold).SetExpansion(1))
	table.SetCell(row, 5, tview.NewTableCell("Mod").SetTextColor(tcell.ColorDarkGray).SetStyle(bold).SetExpansion(1))
	row++
	table.SetCell(row, 0, tview.NewTableCell("..").SetTextColor(tcell.ColorBlue))
	row++
	for _, d := range dirs {
		c := tview.NewTableCell(d.paddedName(maxNameLength)).SetTextColor(tcell.ColorBlue)
		if !strings.HasPrefix(d.Name(), ".") {
			c.SetStyle(bold).SetTextColor(tcell.ColorBlue)
		}
		table.SetCell(row, 0, c)
		table.SetCell(row, 2, tview.NewTableCell(d.paddedUser(maxUserLength)).SetTextColor(tcell.ColorYellow))
		table.SetCell(row, 3, tview.NewTableCell(d.paddedGroup(maxGroupLength)).SetTextColor(tcell.ColorYellow))
		table.SetCell(row, 4, tview.NewTableCell(fmt.Sprintf("%-12s", d.Mode().Perm())).SetTextColor(tcell.ColorYellow))
		table.SetCell(row, 5, tview.NewTableCell(d.ModTime().Format(time.RFC822)).SetTextColor(tcell.ColorDarkGray))
		row++
	}
	for _, irr := range irregulars {
		c := tview.NewTableCell(irr.paddedName(maxNameLength)).SetTextColor(tcell.ColorRed)
		table.SetCell(row, 0, c)
		table.SetCell(row, 2, tview.NewTableCell(irr.paddedUser(maxUserLength)).SetTextColor(tcell.ColorYellow))
		table.SetCell(row, 3, tview.NewTableCell(irr.paddedGroup(maxGroupLength)).SetTextColor(tcell.ColorYellow))
		table.SetCell(row, 4, tview.NewTableCell(fmt.Sprintf("%-12s", irr.Mode().Perm())).SetTextColor(tcell.ColorYellow))
		row++
	}
	for _, reg := range regulars {
		c := tview.NewTableCell(reg.paddedName(maxNameLength))
		if !strings.HasPrefix(reg.Name(), ".") {
			c.SetStyle(bold)
		}
		table.SetCell(row, 0, c)
		table.SetCell(row, 1, tview.NewTableCell(reg.paddedSize(maxSizeLength)))
		table.SetCell(row, 2, tview.NewTableCell(reg.paddedUser(maxUserLength)).SetTextColor(tcell.ColorYellow))
		table.SetCell(row, 3, tview.NewTableCell(reg.paddedGroup(maxGroupLength)).SetTextColor(tcell.ColorYellow))
		table.SetCell(row, 4, tview.NewTableCell(fmt.Sprintf("%-12s", reg.Mode().Perm())).SetTextColor(tcell.ColorYellow))
		table.SetCell(row, 5, tview.NewTableCell(reg.ModTime().Format(time.RFC822)).SetTextColor(tcell.ColorDarkGray))
		row++
	}

	table.SetSelectedFunc(func(row, _ int) {
		if row == 0 {
			return
		}
		if row == 1 {
			selected.Store(SelectedFile{Name: ".."})
			app.Stop()
			return
		}
		f := allfiles[row-2]
		if f.IsDir() || f.Mode().IsRegular() {
			selected.Store(SelectedFile{Name: f.Name(), Size: f.Size(), Mode: f.Mode()})
		}
		app.Stop()
	})

	err := app.SetRoot(table, true).Run()
	return selected.Load().(SelectedFile), err
}

func FormatListOfFiles(width int, long bool, files []Unixfile, buf io.Writer) {
	maxlen := int(1)
	if len(files) != 0 {
		maxlen += linq.From(files).SelectT(func(info Unixfile) int {
			if info.IsDir() {
				return len(info.Path) + 1
			}
			return len(info.Path)
		}).Max().(int)
	}
	padfmt := "%-" + fmt.Sprintf("%d", maxlen) + "s"
	columns := width / maxlen
	if columns == 0 {
		columns = 1
	}
	percolumn := (len(files) / columns) + 1

	sort.Slice(files, func(i, j int) bool {
		return files[i].Path < files[j].Path
	})
	aur := aurora.NewAurora(true)
	var name interface{}
	var lines [][]interface{}
	if long {
		lines = make([][]interface{}, len(files))
	} else {
		lines = make([][]interface{}, percolumn)
	}
	var line int
	for _, f := range files {
		line++
		if line > percolumn && !long {
			line = 1
		}
		if f.IsDir() {
			name = aur.Blue(fmt.Sprintf(padfmt, f.Path+"/"))
		} else {
			name = fmt.Sprintf(padfmt, f.Path)
		}
		if !strings.HasPrefix(f.Path, ".") {
			name = aur.Bold(name)
		}
		lines[line-1] = append(lines[line-1], name)
	}
	for _, line := range lines {
		for _, name := range line {
			fmt.Fprint(buf, name)
		}
		fmt.Fprintln(buf)
	}
}

func ShowFile(fname string, f Stater, external bool) error {
	stats, err := f.Stat()
	if err != nil {
		return err
	}
	if stats.IsDir() {
		return fmt.Errorf("is a directory: %s", fname)
	}
	if !stats.Mode().IsRegular() {
		return fmt.Errorf("not a regular file: %s", fname)
	}
	if !external {
		return showFileInternal(fname, f)
	}
	pager := os.Getenv("PAGER")
	if pager == "" {
		pager = "less"
	}
	p, err := exec.LookPath(pager)
	if err != nil {
		return showFileInternal(fname, f)
	}
	var buf bytes.Buffer
	err = Colorize(fname, f, &buf)
	if err != nil {
		return err
	}
	c := exec.Command(p, "-R")
	c.Stdin = &buf
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr
	err = c.Start()
	if err != nil {
		return showFileInternal(fname, f)
	}
	return c.Wait()
}

func showFileInternal(fname string, f io.Reader) error {
	app := tview.NewApplication()
	app.SetInputCapture(func(ev *tcell.EventKey) *tcell.EventKey {
		if ev.Key() == tcell.KeyCtrlC {
			app.Stop()
			return nil
		}
		return ev
	})
	box := tview.NewTextView().SetScrollable(true).SetWrap(true)
	box.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		key := event.Key()
		if event.Rune() == 'q' || key == tcell.KeyEscape {
			app.Stop()
			return nil
		}
		if key == tcell.KeyEnter {
			return tcell.NewEventKey(tcell.KeyDown, 'j', tcell.ModNone)
		}
		return event
	})
	box.SetBorder(true).SetBorderPadding(1, 1, 1, 1).SetTitle(" " + fname + " ")
	err := Colorize(fname, f, box)
	if err != nil {
		return err
	}
	err = app.SetRoot(box, true).Run()
	if err != nil {
		return err
	}
	return nil
}
