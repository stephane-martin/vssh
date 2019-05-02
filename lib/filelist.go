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

func fillTable(table *tview.Table, allfiles *Unixfiles, app *tview.Application, selected *atomic.Value, callback SelectedCallback, remote bool, pages *tview.Pages) {

	maxNameLength := allfiles.maxNameLength()
	maxSizeLength := allfiles.maxSizeLength()
	maxUserLength := allfiles.maxUserLength()
	maxGroupLength := allfiles.maxGroupLength()

	abort := func(e error) {
		selected.Store(&SelectedFile{Err: e, Action: Stop})
		app.Stop()
	}

	table.Clear()

	table.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		key := event.Key()
		r := event.Rune()
		if r == 'q' || key == tcell.KeyEscape {
			abort(nil)
			return nil
		}
		if key == tcell.KeyEnter {
			position, _ := table.GetSelection()
			f := allfiles.getSelectedFile(position)
			if f == nil {
				return nil
			}
			if f.IsDir {
				f.Action = OpenDir
				files, err := callback(f)
				if err != nil {
					abort(err)
					return nil
				}
				allfiles.Init(files, remote)
				fillTable(table, allfiles, app, selected, callback, remote, pages)
				table.Select(1, 0)
				return nil
			}
			if f.Mode.IsRegular() {
				f.Action = ViewFile
				selected.Store(f)
				app.Stop()
			}
			return nil
		}
		if r == 'r' {
			files, err := callback(nil)
			if err != nil {
				abort(err)
				return nil
			}
			allfiles.Init(files, remote)
			fillTable(table, allfiles, app, selected, callback, remote, pages)
			table.Select(1, 0)
			return nil
		}
		if r == 'o' {
			position, _ := table.GetSelection()
			f := allfiles.getSelectedFile(position)
			if f == nil {
				return nil
			}
			if f.IsDir {
				f.Action = OpenDir
				files, err := callback(f)
				if err != nil {
					abort(err)
					return nil
				}
				allfiles.Init(files, remote)
				fillTable(table, allfiles, app, selected, callback, remote, pages)
				table.Select(1, 0)
				return nil
			}
			if f.Mode.IsRegular() {
				f.Action = OpenFile
				_, err := callback(f)
				if err != nil {
					abort(err)
					return nil
				}
			}
			return nil
		}
		if r == 'D' {
			position, _ := table.GetSelection()
			f := allfiles.getSelectedFile(position)
			if f == nil {
				return nil
			}
			if f.IsDir {
				if f.Name == ".." {
					return nil
				}
				return nil
			}
			if f.Mode.IsRegular() {
				modal := tview.NewModal().
					SetText(fmt.Sprintf("Do you want to delete the file?\n[blue]%s[-]", f.Name)).
					AddButtons([]string{"Delete", "Cancel"}).
					SetDoneFunc(func(buttonIndex int, buttonLabel string) {
						pages.RemovePage("confirmDelete")
						if buttonLabel == "Delete" {
							f.Action = DeleteFile
							files, err := callback(f)
							if err != nil {
								abort(err)
								return
							}
							allfiles.Init(files, remote)
							fillTable(table, allfiles, app, selected, callback, remote, pages)
							table.Select(1, 0)
						}
					})
				pages.AddPage("confirmDelete", modal, true, true)
			}
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
	for _, d := range allfiles.Dirs {
		c := tview.NewTableCell(d.paddedName(maxNameLength))
		if strings.HasPrefix(d.Name(), ".") {
			c.SetTextColor(tcell.ColorBlue)
		} else {
			c.SetStyle(bold).SetTextColor(tcell.ColorBlue)
		}
		table.SetCell(row, 0, c)
		table.SetCell(row, 2, tview.NewTableCell(d.paddedUser(maxUserLength)).SetTextColor(tcell.ColorYellow))
		table.SetCell(row, 3, tview.NewTableCell(d.paddedGroup(maxGroupLength)).SetTextColor(tcell.ColorYellow))
		table.SetCell(row, 4, tview.NewTableCell(fmt.Sprintf("%-12s", d.Mode().Perm())).SetTextColor(tcell.ColorYellow))
		table.SetCell(row, 5, tview.NewTableCell(d.ModTime().Format(time.RFC822)).SetTextColor(tcell.ColorDarkGray))
		row++
	}
	for _, irr := range allfiles.Irregulars {
		c := tview.NewTableCell(irr.paddedName(maxNameLength)).SetTextColor(tcell.ColorRed)
		table.SetCell(row, 0, c)
		table.SetCell(row, 2, tview.NewTableCell(irr.paddedUser(maxUserLength)).SetTextColor(tcell.ColorYellow))
		table.SetCell(row, 3, tview.NewTableCell(irr.paddedGroup(maxGroupLength)).SetTextColor(tcell.ColorYellow))
		table.SetCell(row, 4, tview.NewTableCell(fmt.Sprintf("%-12s", irr.Mode().Perm())).SetTextColor(tcell.ColorYellow))
		row++
	}
	for _, reg := range allfiles.Regulars {
		c := tview.NewTableCell(reg.paddedName(maxNameLength))
		color := tcell.ColorDefault
		if (reg.Mode().Perm() & 0100) != 0 {
			color = tcell.ColorGreen
		}
		if strings.HasPrefix(reg.Name(), ".") {
			c.SetTextColor(color)
		} else {
			c.SetStyle(bold).SetTextColor(color)
		}
		table.SetCell(row, 0, c)
		table.SetCell(row, 1, tview.NewTableCell(reg.paddedSize(maxSizeLength)))
		table.SetCell(row, 2, tview.NewTableCell(reg.paddedUser(maxUserLength)).SetTextColor(tcell.ColorYellow))
		table.SetCell(row, 3, tview.NewTableCell(reg.paddedGroup(maxGroupLength)).SetTextColor(tcell.ColorYellow))
		table.SetCell(row, 4, tview.NewTableCell(fmt.Sprintf("%-12s", reg.Mode().Perm())).SetTextColor(tcell.ColorYellow))
		table.SetCell(row, 5, tview.NewTableCell(reg.ModTime().Format(time.RFC822)).SetTextColor(tcell.ColorDarkGray))
		row++
	}

	table.SetSelectedFunc(func(position, _ int) {
		f := allfiles.getSelectedFile(position)
		if f == nil {
			return
		}
		if f.IsDir {
			f.Action = OpenDir
		} else {
			f.Action = ViewFile
		}
		selected.Store(f)
		app.Stop()
	})

}

func TableOfFiles(wd string, callback SelectedCallback, position int, remote bool) (*SelectedFile, error) {
	// TODO: modtime
	var selected atomic.Value
	files, err := callback(nil)
	if err != nil {
		return nil, err
	}

	allfiles := new(Unixfiles)
	allfiles.Init(files, remote)

	app := tview.NewApplication()
	app.SetInputCapture(func(ev *tcell.EventKey) *tcell.EventKey {
		if ev.Key() == tcell.KeyCtrlC {
			selected.Store(&SelectedFile{Action: Stop})
			app.Stop()
			return nil
		}
		return ev
	})

	table := tview.NewTable().SetBorders(false).SetFixed(1, 0)
	pages := tview.NewPages()
	pages.AddPage("table", table, true, true)
	table.SetSelectable(true, false)
	table.SetSelectedStyle(tcell.ColorRed, tcell.ColorDefault, tcell.AttrBold)
	table.SetBorder(true).SetBorderPadding(1, 0, 1, 1).SetTitle(" " + wd + " ")
	fillTable(table, allfiles, app, &selected, callback, remote, pages)
	table.Select(position, 0)

	err = app.SetRoot(pages, true).Run()
	s := selected.Load()
	if s == nil {
		return nil, nil
	}
	s2 := s.(*SelectedFile)
	if s2.Err != nil {
		return nil, s2.Err
	}
	return s2, err
}

func FormatListOfFiles(width int, long bool, files []Unixfile, buf io.Writer) {
	// TODO: long should return more information
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
		} else if f.Mode().IsRegular() {
			if (f.Mode().Perm() & 0100) != 0 {
				name = aur.Green(fmt.Sprintf(padfmt, f.Path))
			} else {
				name = fmt.Sprintf(padfmt, f.Path)
			}
		} else {
			name = aur.Red(fmt.Sprintf(padfmt, f.Path))
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

func ShowFile(fname string, content []byte, external bool) error {
	if !external {
		return showFileInternal(fname, content)
	}
	pager := os.Getenv("PAGER")
	if pager == "" {
		pager = "less"
	}
	p, err := exec.LookPath(pager)
	if err != nil {
		return showFileInternal(fname, content)
	}
	var buf bytes.Buffer
	err = Colorize(fname, content, &buf)
	if err != nil {
		return err
	}
	c := exec.Command(p, "-R")
	c.Stdin = &buf
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr
	err = c.Start()
	if err != nil {
		return showFileInternal(fname, content)
	}
	return c.Wait()
}

func showFileInternal(fname string, content []byte) error {
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
	err := Colorize(fname, content, box)
	if err != nil {
		return err
	}
	err = app.SetRoot(box, true).Run()
	if err != nil {
		return err
	}
	return nil
}
