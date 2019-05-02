package lib

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sort"
	"strings"
	"time"

	"github.com/ahmetb/go-linq"
	"github.com/gdamore/tcell"
	"github.com/logrusorgru/aurora"
	"github.com/rivo/tview"
)

type tableOfFiles struct {
	app      *tview.Application
	pages    *tview.Pages
	table    *tview.Table
	files    *Unixfiles
	rerr     chan error
	callback SelectedCallback
	readFile func(string) ([]byte, error)
	remote   bool
}

func (table *tableOfFiles) fill() {

	maxNameLength := table.files.maxNameLength()
	maxSizeLength := table.files.maxSizeLength()
	maxUserLength := table.files.maxUserLength()
	maxGroupLength := table.files.maxGroupLength()

	abort := func(e error) {
		if e != nil {
			table.rerr <- e
		}
		close(table.rerr)
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
					return event
				})
				w.SetDoneFunc(func(_ tcell.Key) {
					table.pages.RemovePage("less")
				})
				table.pages.AddPage("less", w, true, true)
			}
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
	table.table.SetCell(row, 0, tview.NewTableCell("..").SetTextColor(tcell.ColorBlue))
	row++
	for _, d := range table.files.Dirs {
		c := tview.NewTableCell(d.paddedName(maxNameLength))
		if strings.HasPrefix(d.Name(), ".") {
			c.SetTextColor(tcell.ColorBlue)
		} else {
			c.SetStyle(bold).SetTextColor(tcell.ColorBlue)
		}
		table.table.SetCell(row, 0, c)
		table.table.SetCell(row, 2, tview.NewTableCell(d.paddedUser(maxUserLength)).SetTextColor(tcell.ColorYellow))
		table.table.SetCell(row, 3, tview.NewTableCell(d.paddedGroup(maxGroupLength)).SetTextColor(tcell.ColorYellow))
		table.table.SetCell(row, 4, tview.NewTableCell(fmt.Sprintf("%-12s", d.Mode().Perm())).SetTextColor(tcell.ColorYellow))
		table.table.SetCell(row, 5, tview.NewTableCell(d.ModTime().Format(time.RFC822)).SetTextColor(tcell.ColorDarkGray))
		row++
	}
	for _, irr := range table.files.Irregulars {
		c := tview.NewTableCell(irr.paddedName(maxNameLength)).SetTextColor(tcell.ColorRed)
		table.table.SetCell(row, 0, c)
		table.table.SetCell(row, 2, tview.NewTableCell(irr.paddedUser(maxUserLength)).SetTextColor(tcell.ColorYellow))
		table.table.SetCell(row, 3, tview.NewTableCell(irr.paddedGroup(maxGroupLength)).SetTextColor(tcell.ColorYellow))
		table.table.SetCell(row, 4, tview.NewTableCell(fmt.Sprintf("%-12s", irr.Mode().Perm())).SetTextColor(tcell.ColorYellow))
		row++
	}
	for _, reg := range table.files.Regulars {
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
		table.table.SetCell(row, 0, c)
		table.table.SetCell(row, 1, tview.NewTableCell(reg.paddedSize(maxSizeLength)))
		table.table.SetCell(row, 2, tview.NewTableCell(reg.paddedUser(maxUserLength)).SetTextColor(tcell.ColorYellow))
		table.table.SetCell(row, 3, tview.NewTableCell(reg.paddedGroup(maxGroupLength)).SetTextColor(tcell.ColorYellow))
		table.table.SetCell(row, 4, tview.NewTableCell(fmt.Sprintf("%-12s", reg.Mode().Perm())).SetTextColor(tcell.ColorYellow))
		table.table.SetCell(row, 5, tview.NewTableCell(reg.ModTime().Format(time.RFC822)).SetTextColor(tcell.ColorDarkGray))
		row++
	}

}

func TableOfFiles(wd string, callback SelectedCallback, readFile func(string) ([]byte, error), remote bool) error {
	// TODO: modtime
	table := new(tableOfFiles)
	table.callback = callback
	table.readFile = readFile
	table.remote = remote
	table.rerr = make(chan error)
	files, err := callback(nil)
	if err != nil {
		return err
	}
	table.files = new(Unixfiles)
	table.files.Init(files, remote)

	table.app = tview.NewApplication()
	table.app.SetInputCapture(func(ev *tcell.EventKey) *tcell.EventKey {
		if ev.Key() == tcell.KeyCtrlC {
			close(table.rerr)
			table.app.Stop()
			return nil
		}
		return ev
	})

	table.table = tview.NewTable().SetBorders(false).SetFixed(1, 0)
	table.pages = tview.NewPages()
	table.pages.AddPage("table", table.table, true, true)
	table.table.SetSelectable(true, false)
	table.table.SetSelectedStyle(tcell.ColorRed, tcell.ColorDefault, tcell.AttrBold)
	table.table.SetBorder(true).SetBorderPadding(1, 0, 1, 1).SetTitle(" " + wd + " ")
	table.fill()
	table.table.Select(1, 0)

	err = table.app.SetRoot(table.pages, true).Run()
	if err != nil {
		return err
	}
	return <-table.rerr
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
		return ShowFileInternal(fname, content)
	}
	pager := os.Getenv("PAGER")
	if pager == "" {
		pager = "less"
	}
	p, err := exec.LookPath(pager)
	if err != nil {
		return ShowFileInternal(fname, content)
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
		return ShowFileInternal(fname, content)
	}
	return c.Wait()
}

func ShowFileInternal(fname string, content []byte) error {
	app := tview.NewApplication()
	app.SetInputCapture(func(ev *tcell.EventKey) *tcell.EventKey {
		if ev.Key() == tcell.KeyCtrlC || ev.Key() == tcell.KeyESC || ev.Rune() == 'q' {
			app.Stop()
			return nil
		}
		return ev
	})
	w, err := ShowFileInternalWidget(fname, content)
	if err != nil {
		return err
	}
	return app.SetRoot(w, true).Run()
}

func ShowFileInternalWidget(fname string, content []byte) (*tview.TextView, error) {
	box := tview.NewTextView().SetScrollable(true).SetWrap(true)
	box.SetBorder(true).SetBorderPadding(1, 1, 1, 1).SetTitle(" " + fname + " ")
	box.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if event.Key() == tcell.KeyEnter {
			return tcell.NewEventKey(tcell.KeyDown, 'j', tcell.ModNone)
		}
		return event
	})

	err := Colorize(fname, content, box)
	if err != nil {
		return nil, err
	}
	return box, nil
}
