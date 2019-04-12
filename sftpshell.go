package main

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"sort"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/ahmetb/go-linq"
	"github.com/alecthomas/chroma/formatters"
	"github.com/alecthomas/chroma/lexers"
	"github.com/alecthomas/chroma/styles"
	"github.com/gdamore/tcell"
	"github.com/logrusorgru/aurora"
	"github.com/mattn/go-shellwords"
	"github.com/mitchellh/go-homedir"
	"github.com/pkg/sftp"
	"github.com/rivo/tview"
	"github.com/stephane-martin/vssh/lib"
	"golang.org/x/crypto/ssh/terminal"
)

type command func([]string) (string, error)

type cmpl func([]string) []string

type shellstate struct {
	LocalWD       string
	RemoteWD      string
	initRemoteWD  string
	client        *sftp.Client
	methods       map[string]command
	completes     map[string]cmpl
	externalPager bool
	info          func(string)
	err           func(string)
}

func newShellState(client *sftp.Client, externalPager bool, infoFunc func(string), errFunc func(string)) (*shellstate, error) {
	localwd, err := os.Getwd()
	if err != nil {
		return nil, err
	}
	remotewd, err := client.Getwd()
	if err != nil {
		return nil, err
	}
	s := &shellstate{
		LocalWD:       localwd,
		RemoteWD:      remotewd,
		initRemoteWD:  remotewd,
		client:        client,
		externalPager: externalPager,
		info:          infoFunc,
		err:           errFunc,
	}
	s.methods = map[string]command{
		"less":   s.less,
		"lless":  s.lless,
		"lls":    s.lls,
		"ls":     s.ls,
		"ll":     s.ll,
		"lll":    s.lll,
		"lcd":    s.lcd,
		"cd":     s.cd,
		"exit":   s.exit,
		"logout": s.exit,
		"pwd":    s.pwd,
		"lpwd":   s.lpwd,
		"put":    s.put,
	}
	s.completes = map[string]cmpl{
		"lcd":   s.completeLcd,
		"cd":    s.completeCd,
		"less":  s.completeLess,
		"lless": s.completeLless,
	}
	return s, nil
}

func (s *shellstate) width() int {
	width, _, err := terminal.GetSize(int(os.Stdout.Fd()))
	if err != nil {
		return 80
	}
	return width
}

func (s *shellstate) exit(_ []string) (string, error) {
	return "", io.EOF
}

func (s *shellstate) complete(cmd string, args []string) []string {
	fun := s.completes[cmd]
	if fun == nil {
		return nil
	}
	return fun(args)
}

func (s *shellstate) dispatch(line string) (string, error) {
	line = strings.TrimSpace(line)
	if line == "" {
		return "", nil
	}

	p := shellwords.NewParser()
	args, err := p.Parse(line)
	if err != nil {
		return "", err
	}
	if p.Position != -1 {
		return "", errors.New("incomplete parsing error")
	}
	cmd := strings.ToLower(args[0])
	fun := s.methods[cmd]
	if fun == nil {
		return "", fmt.Errorf("unknown command: %s", cmd)
	}
	return fun(args[1:])
}

func join(dname, fname string) string {
	if strings.HasPrefix(fname, "/") {
		return fname
	}
	if strings.HasSuffix(fname, "/") {
		return filepath.Join(dname, fname) + "/"
	}
	return filepath.Join(dname, fname)
}

func (s *shellstate) less(args []string) (string, error) {
	if len(args) != 1 {
		return "", errors.New("less takes one argument")
	}
	fname := join(s.RemoteWD, args[0])
	f, err := s.client.Open(fname)
	if err != nil {
		return "", err
	}
	defer func() { _ = f.Close() }()
	return "", showFile(fname, f, s.externalPager)
}

func (s *shellstate) lless(args []string) (string, error) {
	if len(args) != 1 {
		return "", errors.New("less takes one argument")
	}
	fname := join(s.LocalWD, args[0])
	f, err := os.Open(fname)
	if err != nil {
		return "", err
	}
	defer func() { _ = f.Close() }()
	return "", showFile(fname, f, s.externalPager)
}

type stater interface {
	io.Reader
	Stat() (os.FileInfo, error)
}

func showFile(fname string, f stater, external bool) error {
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
	_ = colorize(fname, f, &buf)
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
	err := colorize(fname, f, box)
	if err != nil {
		// TODO
	}
	err = app.SetRoot(box, true).Run()
	if err != nil {
		return err
	}
	return nil
}

func (s *shellstate) put(args []string) (string, error) {
	localWD := s.LocalWD
	if len(args) == 0 {
		names, err := lib.FuzzyLocal(localWD, nil)
		if err != nil {
			return "", err
		}
		if len(names) == 0 {
			return "", nil
		}
		args = names
	}
	// check all files exist locally
	var files, dirs []string
	for _, name := range args {
		name = join(localWD, name)
		stats, err := os.Stat(name)
		if err != nil {
			return "", err
		}
		if stats.IsDir() {
			dirs = append(dirs, name)
		} else if stats.Mode().IsRegular() {
			files = append(files, name)
		} else {
			return "", fmt.Errorf("not a regular file: %s", name)
		}
	}
	remoteWD := s.RemoteWD
	for _, name := range dirs {
		err := s.putdir(remoteWD, name)
		if err != nil {
			s.err(fmt.Sprintf("upload %s: %s", name, err))
		}
	}
	for _, name := range files {
		err := s.putfile(remoteWD, name)
		if err != nil {
			s.err(fmt.Sprintf("upload %s: %s", name, err))
		}
	}
	return "", nil
}

func (s *shellstate) putfile(targetRemoteDir string, localFile string) error {
	remoteFilename := join(targetRemoteDir, base(localFile))
	source, err := os.Open(localFile)
	if err != nil {
		return err
	}
	defer func() { _ = source.Close() }()
	dest, err := s.client.Create(remoteFilename)
	if err != nil {
		return err
	}
	defer func() { _ = dest.Close() }()
	_, err = io.Copy(dest, source)
	if err != nil {
		return err
	}
	s.info(fmt.Sprintf("uploaded %s", localFile))
	return nil
}

func (s *shellstate) putdir(targetRemoteDir, localDir string) error {
	d, err := os.Open(localDir)
	if err != nil {
		return err
	}
	files, err := d.Readdir(0)
	_ = d.Close()
	if err != nil {
		return err
	}
	newDirname := join(targetRemoteDir, base(localDir))
	err = s.client.Mkdir(newDirname)
	if err != nil && !os.IsExist(err) {
		return err
	}

	for _, f := range files {
		fname := join(localDir, f.Name())
		if f.IsDir() {
			err := s.putdir(newDirname, fname)
			if err != nil {
				s.err(fmt.Sprintf("upload %s: %s", fname, err))
			}
		} else if f.Mode().IsRegular() {
			err := s.putfile(newDirname, fname)
			if err != nil {
				s.err(fmt.Sprintf("upload %s: %s", fname, err))
			}
		}
	}
	s.info(fmt.Sprintf("uploaded %s", localDir))
	return nil
}

func (s *shellstate) pwd(args []string) (string, error) {
	if len(args) != 0 {
		return "", errors.New("pwd takes no argument")
	}
	return s.RemoteWD, nil
}

func (s *shellstate) lpwd(args []string) (string, error) {
	if len(args) != 0 {
		return "", errors.New("lpwd takes no argument")
	}
	return s.LocalWD, nil
}

func (s *shellstate) lcd(args []string) (string, error) {
	if len(args) > 1 {
		return "", errors.New("lcd takes only one argument")
	}
	if len(args) == 0 {
		name, err := homedir.Dir()
		if err != nil {
			return "", err
		}
		args = append(args, name)
	}
	d := join(s.LocalWD, args[0])
	stats, err := os.Stat(d)
	if err != nil {
		return "", err
	}
	if !stats.IsDir() {
		return "", errors.New("not a directory")
	}
	f, err := os.Open(d)
	_ = f.Close()
	if err != nil {
		return "", err
	}
	s.LocalWD = d
	return "", nil
}

func (s *shellstate) cd(args []string) (string, error) {
	if len(args) > 1 {
		return "", errors.New("cd takes only one argument")
	}
	if len(args) == 0 {
		args = append(args, s.initRemoteWD)
	}
	d := join(s.RemoteWD, args[0])
	stats, err := s.client.Stat(d)
	if err != nil {
		return "", err
	}
	if !stats.IsDir() {
		return "", errors.New("not a directory")
	}
	f, err := s.client.Open(d)
	if err != nil {
		return "", err
	}
	_ = f.Close()
	s.RemoteWD = d
	return "", nil
}

func (s *shellstate) lls(args []string) (string, error) {
	c, err := os.Open(s.LocalWD)
	if err != nil {
		return "", fmt.Errorf("error listing directory: %s", err)
	}
	files, err := c.Readdir(0)
	_ = c.Close()
	if err != nil {
		return "", fmt.Errorf("error listing directory: %s", err)
	}
	if len(files) == 0 {
		fmt.Println()
		return "", nil
	}
	return formatListOfFiles(s.width(), false, files)
}

func (s *shellstate) lll(args []string) (string, error) {
	for {
		c, err := os.Open(s.LocalWD)
		if err != nil {
			return "", fmt.Errorf("error listing directory: %s", err)
		}
		files, err := c.Readdir(0)
		_ = c.Close()
		if err != nil {
			return "", fmt.Errorf("error listing directory: %s", err)
		}
		selected, err := tableOfFiles(s.LocalWD, files)
		if err != nil {
			return "", err
		}
		if selected.name == "" {
			return "", nil
		}
		if selected.name == ".." {
			_, err := s.lcd([]string{".."})
			if err != nil {
				return "", err
			}
		} else if selected.mode.IsDir() {
			_, err := s.lcd([]string{selected.name})
			if err != nil {
				return "", err
			}
		} else {
			_, err := s.lless([]string{selected.name})
			if err != nil {
				return "", err
			}
		}
	}
}

func (s *shellstate) ll(args []string) (string, error) {
	for {
		files, err := s.client.ReadDir(s.RemoteWD)
		if err != nil {
			return "", fmt.Errorf("error listing directory: %s", err)
		}
		selected, err := tableOfFiles(s.RemoteWD, files)
		if err != nil {
			return "", err
		}
		if selected.name == "" {
			return "", nil
		}
		if selected.name == ".." {
			_, err := s.cd([]string{".."})
			if err != nil {
				return "", err
			}
		} else if selected.mode.IsDir() {
			_, err := s.cd([]string{selected.name})
			if err != nil {
				return "", err
			}
		} else {
			_, err := s.less([]string{selected.name})
			if err != nil {
				return "", err
			}
		}
	}
}

func (s *shellstate) ls(args []string) (string, error) {
	files, err := s.client.ReadDir(s.RemoteWD)
	if err != nil {
		return "", fmt.Errorf("error listing directory: %s", err)
	}
	if len(files) == 0 {
		fmt.Println()
		return "", nil
	}
	return formatListOfFiles(s.width(), false, files)
}

func (s *shellstate) completeLess(args []string) []string {
	if len(args) > 1 {
		return nil
	}
	var input string
	if len(args) == 1 {
		input = args[0]
	}
	cand, dirname, relDirname := candidate(s.RemoteWD, input)
	files, err := s.client.ReadDir(dirname)
	if err != nil {
		return nil
	}

	props := completeFiles(cand, files, false, false)
	if len(props) == 0 {
		return nil
	}
	linq.From(props).SelectT(func(s string) string {
		return join(relDirname, s)
	}).ToSlice(&props)
	return props
}

func base(s string) string {
	s = filepath.Base(s)
	if s == "/" {
		return ""
	}
	return s
}

func candidate(wd, input string) (cand, dirname, relDirname string) {
	var err error
	if input == "" {
		return "", wd, ""
	}
	if strings.HasSuffix(input, "/") {
		cand = ""
		dirname = join(wd, input)
	} else {
		cand = base(input)
		dirname = filepath.Dir(join(wd, input))
	}
	relDirname = dirname
	if !strings.HasPrefix(input, "/") {
		relDirname, err = filepath.Rel(wd, dirname)
		if err != nil {
			relDirname = dirname
		}
	}
	return cand, dirname, relDirname
}

func (s *shellstate) completeLless(args []string) []string {
	if len(args) > 1 {
		return nil
	}
	var input string
	if len(args) == 1 {
		input = args[0]
	}
	cand, dirname, relDirname := candidate(s.LocalWD, input)
	c, err := os.Open(dirname)
	if err != nil {
		return nil
	}
	files, err := c.Readdir(0)
	_ = c.Close()
	if err != nil {
		return nil
	}
	props := completeFiles(cand, files, false, false)
	if len(props) == 0 {
		return nil
	}
	linq.From(props).SelectT(func(s string) string {
		return join(relDirname, s)
	}).ToSlice(&props)
	return props
}

func (s *shellstate) completeLcd(args []string) []string {
	if len(args) > 1 {
		return nil
	}
	var input string
	if len(args) == 1 {
		input = args[0]
	}
	cand, dirname, relDirname := candidate(s.LocalWD, input)
	c, err := os.Open(dirname)
	if err != nil {
		return nil
	}
	files, err := c.Readdir(0)
	_ = c.Close()
	if err != nil {
		return nil
	}
	props := completeFiles(cand, files, true, false)
	if len(props) == 0 {
		return nil
	}
	linq.From(props).SelectT(func(s string) string {
		return join(relDirname, s)
	}).ToSlice(&props)
	return props
}

func (s *shellstate) completeCd(args []string) []string {
	if len(args) > 1 {
		return nil
	}
	var input string
	if len(args) == 1 {
		input = args[0]
	}
	cand, dirname, relDirname := candidate(s.RemoteWD, input)
	files, err := s.client.ReadDir(dirname)
	if err != nil {
		return nil
	}
	props := completeFiles(cand, files, true, false)
	if len(props) == 0 {
		return nil
	}
	linq.From(props).SelectT(func(s string) string {
		return join(relDirname, s)
	}).ToSlice(&props)
	return props
}

func completeFiles(candidate string, files []os.FileInfo, onlyDirs, onlyFiles bool) []string {
	var props []string

	if onlyDirs {
		linq.From(files).
			WhereT(func(info os.FileInfo) bool {
				return info.IsDir()
			}).
			SelectT(func(info os.FileInfo) string { return info.Name() + "/" }).
			ToSlice(&props)
	} else if onlyFiles {
		linq.From(files).
			WhereT(func(info os.FileInfo) bool { return info.Mode().IsRegular() }).
			SelectT(func(info os.FileInfo) string { return info.Name() }).
			ToSlice(&props)
	} else {
		linq.From(files).
			SelectT(func(info os.FileInfo) string {
				if info.IsDir() {
					return info.Name() + "/"
				}
				return info.Name()
			}).
			ToSlice(&props)
	}
	if candidate != "" {
		linq.From(props).WhereT(func(s string) bool { return strings.HasPrefix(s, candidate) }).ToSlice(&props)
	}
	if len(props) == 0 {
		return nil
	}
	linq.From(props).SelectT(func(p string) string {
		var buf bytes.Buffer
		quote(p, &buf)
		return buf.String()
	}).ToSlice(&props)
	return props
}

func userGroup(info os.FileInfo) (string, string) {
	if i, ok := info.Sys().(*syscall.Stat_t); ok {
		u, err := user.LookupId(fmt.Sprintf("%d", i.Uid))
		if err != nil {
			return "", ""
		}
		g, err := user.LookupGroupId(fmt.Sprintf("%d", i.Gid))
		if err != nil {
			return "", ""
		}
		return u.Username, g.Name
	}
	return "", ""
}

type fileStat struct {
	name string
	size int64
	mode os.FileMode
}

type unixfile struct {
	os.FileInfo
	user  string
	group string
}

func (f unixfile) paddedName(l int) string {
	return fmt.Sprintf("%-"+fmt.Sprintf("%d", l)+"s", f.Name())
}

func (f unixfile) paddedSize(l int) string {
	return fmt.Sprintf("%-"+fmt.Sprintf("%d", l)+"d", f.Size())
}

func (f unixfile) paddedUser(l int) string {
	return fmt.Sprintf("%-"+fmt.Sprintf("%d", l)+"s", f.user)
}

func (f unixfile) paddedGroup(l int) string {
	return fmt.Sprintf("%-"+fmt.Sprintf("%d", l)+"s", f.group)
}

type unixfiles []unixfile

func (files unixfiles) maxNameLength() int {
	return linq.From(files).SelectT(func(file unixfile) int { return len(file.Name()) }).Max().(int)
}

func (files unixfiles) maxSizeLength() int {
	return linq.From(files).SelectT(func(file unixfile) int { return len(fmt.Sprintf("%d", file.Size())) }).Max().(int)
}

func (files unixfiles) maxUserLength() int {
	return linq.From(files).SelectT(func(file unixfile) int { return len(file.user) }).Max().(int)
}

func (files unixfiles) maxGroupLength() int {
	return linq.From(files).SelectT(func(file unixfile) int { return len(file.group) }).Max().(int)
}

func tableOfFiles(dirname string, files []os.FileInfo) (fileStat, error) {
	var selected atomic.Value
	selected.Store(fileStat{})
	var dirs, regulars, irregulars unixfiles
	for _, f := range files {
		u, g := userGroup(f)
		if f.IsDir() {
			dirs = append(dirs, unixfile{FileInfo: f, user: u, group: g})
		} else if f.Mode().IsRegular() {
			regulars = append(regulars, unixfile{FileInfo: f, user: u, group: g})
		} else {
			irregulars = append(irregulars, unixfile{FileInfo: f, user: u, group: g})
		}
	}
	sort.Slice(dirs, func(i, j int) bool { return dirs[i].Name() < dirs[j].Name() })
	sort.Slice(regulars, func(i, j int) bool { return regulars[i].Name() < regulars[j].Name() })
	sort.Slice(irregulars, func(i, j int) bool { return irregulars[i].Name() < regulars[j].Name() })

	var allfiles unixfiles = make([]unixfile, 0, len(dirs)+len(regulars)+len(irregulars))
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
			c.SetStyle(bold)
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
			selected.Store(fileStat{name: ".."})
			app.Stop()
			return
		}
		f := allfiles[row-2]
		if f.IsDir() || f.Mode().IsRegular() {
			selected.Store(fileStat{name: f.Name(), size: f.Size(), mode: f.Mode()})
		}
		app.Stop()
	})

	err := app.SetRoot(table, true).Run()
	return selected.Load().(fileStat), err
}

func formatListOfFiles(width int, long bool, files []os.FileInfo) (string, error) {
	maxlen := linq.From(files).SelectT(func(info os.FileInfo) int {
		if info.IsDir() {
			return len(info.Name()) + 1
		}
		return len(info.Name())
	}).Max().(int) + 1
	padfmt := "%-" + fmt.Sprintf("%d", maxlen) + "s"
	columns := width / maxlen
	if columns == 0 {
		columns = 1
	}
	percolumn := (len(files) / columns) + 1

	sort.Slice(files, func(i, j int) bool {
		return files[i].Name() < files[j].Name()
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
			name = aur.Blue(fmt.Sprintf(padfmt, f.Name()+"/"))
		} else {
			name = fmt.Sprintf(padfmt, f.Name())
		}
		if !strings.HasPrefix(f.Name(), ".") {
			name = aur.Bold(name)
		}
		lines[line-1] = append(lines[line-1], name)
	}
	var buf strings.Builder
	for _, line := range lines {
		for _, name := range line {
			fmt.Fprint(&buf, name)
		}
		fmt.Fprintln(&buf)
	}
	return buf.String(), nil

}

func colorize(name string, text io.Reader, out io.Writer) error {
	ext := strings.ToLower(filepath.Ext(name))
	if ext == ".pdf" {
		return pdftotext(text, out)
	}
	lexer := lexers.Match(filepath.Base(name))
	if lexer == nil {
		_, _ = io.Copy(out, text)
		return errors.New("lexer not found")
	}
	styleName := os.Getenv("VSSH_THEME")
	if styleName == "" {
		styleName = "monokai"
	}
	style := styles.Get(styleName)
	if style == nil {
		_, _ = io.Copy(out, text)
		return errors.New("style not found")
	}
	formatter := formatters.Get("terminal256")
	if formatter == nil {
		_, _ = io.Copy(out, text)
		return errors.New("formatter not found")
	}
	t, err := ioutil.ReadAll(text)
	if err != nil {
		return err
	}
	iterator, err := lexer.Tokenise(nil, string(t))
	if err != nil {
		_, _ = out.Write(t)
		return err
	}
	if box, ok := out.(*tview.TextView); ok {
		box.SetDynamicColors(true)
		out = tview.ANSIWriter(out)
	}
	return formatter.Format(out, style, iterator)
}

func pdftotext(content io.Reader, out io.Writer) error {
	p, err := exec.LookPath("pdftotext")
	if err != nil {
		return err
	}
	temp, err := ioutil.TempFile("", "vssh-temp-*.pdf")
	if err != nil {
		return err
	}
	path := temp.Name()
	defer func() {
		_ = temp.Close()
		_ = os.Remove(path)
	}()
	_, err = io.Copy(temp, content)
	if err != nil {
		return err
	}
	_ = temp.Close()
	cmd := exec.Command(p, "-q", "-nopgbrk", "-enc", "UTF-8", "-eol", "unix", path, "-")
	cmd.Stdout = out
	return cmd.Run()
}
