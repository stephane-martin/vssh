package widgets

import (
	"bytes"
	"fmt"
	"github.com/gdamore/tcell"
	"github.com/rivo/tview"
	"os"
	"os/exec"
)

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
	box.SetBorder(true).SetBorderPadding(1, 1, 1, 1).SetTitle(fmt.Sprintf(" [violet]%s[-] ", fname))
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



type SelectedAction uint8

type SelectedCallback func(*SelectedFile) ([]os.FileInfo, error)

// TODO: download, upload, delete, edit, help

const (
	Init SelectedAction = iota
	OpenDir
	OpenFile
	DeleteFile
	DeleteDir
	EditFile
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