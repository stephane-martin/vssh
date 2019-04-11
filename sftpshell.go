package main

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/ahmetb/go-linq"
	"github.com/alecthomas/chroma/formatters"
	"github.com/alecthomas/chroma/lexers"
	"github.com/alecthomas/chroma/styles"
	"github.com/gdamore/tcell"
	"github.com/logrusorgru/aurora"
	"github.com/mattn/go-shellwords"
	"github.com/pkg/sftp"
	"github.com/rivo/tview"
	"golang.org/x/crypto/ssh/terminal"
)

type command func([]string) (string, error)

type cmpl func([]string) []string

type shellstate struct {
	localWD   string
	remoteWD  string
	client    *sftp.Client
	methods   map[string]command
	completes map[string]cmpl
}

func newShellState(client *sftp.Client) (*shellstate, error) {
	localwd, err := os.Getwd()
	if err != nil {
		return nil, err
	}
	remotewd, err := client.Getwd()
	if err != nil {
		return nil, err
	}
	s := &shellstate{
		localWD:  localwd,
		remoteWD: remotewd,
		client:   client,
	}
	s.methods = map[string]command{
		"less":   s.less,
		"lless":  s.lless,
		"lls":    s.lls,
		"ls":     s.ls,
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
		return "", fmt.Errorf("parsing error: %s", err)
	}
	if p.Position != -1 {
		return "", errors.New("parsing error")
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
	return filepath.Join(dname, fname)
}

func (s *shellstate) less(args []string) (string, error) {
	if len(args) != 1 {
		return "", errors.New("less takes one argument")
	}
	fname := join(s.remoteWD, args[0])
	f, err := s.client.Open(fname)
	if err != nil {
		return "", err
	}
	defer func() { _ = f.Close() }()
	return "", showFile(fname, f)
}

func (s *shellstate) lless(args []string) (string, error) {
	if len(args) != 1 {
		return "", errors.New("less takes one argument")
	}
	fname := join(s.localWD, args[0])
	f, err := os.Open(fname)
	if err != nil {
		return "", err
	}
	defer func() { _ = f.Close() }()
	return "", showFile(fname, f)
}

func showFile(fname string, f io.Reader) error {
	app := tview.NewApplication()
	box := tview.NewTextView().SetScrollable(true).SetWrap(true).SetDoneFunc(func(_ tcell.Key) { app.Stop() })
	box.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if event.Rune() == 'q' {
			app.Stop()
			return nil
		}
		return event
	})
	box.SetBorder(true).SetBorderPadding(1, 1, 1, 1).SetTitle(" " + fname + " ")
	box.SetDynamicColors(true)
	text, err := ioutil.ReadAll(f)
	if err != nil {
		return err
	}
	_, err = io.WriteString(box, colorize(fname, string(text)))
	if err != nil {
		return err
	}
	err = app.SetRoot(box, true).Run()
	if err != nil {
		return err
	}
	return nil
}

func (s *shellstate) put(args []string) (string, error) {
	localWD := s.localWD
	if len(args) == 0 {
		// TODO: fuzzyfinder
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
	remoteWD := s.remoteWD
	for _, name := range dirs {
		err := s.putdir(name)
		if err != nil {
			return "", err
		}
	}
	for _, name := range files {
		err := s.putfile(remoteWD, name)
		if err != nil {
			return "", err
		}
	}
	return "", nil
}

func (s *shellstate) putfile(remotedir string, fname string) error {
	basename := filepath.Base(fname)
	remotename := filepath.Join(remotedir, basename)
	source, err := os.Open(fname)
	if err != nil {
		return err
	}
	defer func() { _ = source.Close() }()
	dest, err := s.client.Create(remotename)
	if err != nil {
		return err
	}
	defer func() { _ = dest.Close() }()
	_, err = io.Copy(dest, source)
	if err != nil {
		return err
	}
	return nil
}

func (s *shellstate) putdir(dname string) error {
	return nil
}

func (s *shellstate) pwd(args []string) (string, error) {
	if len(args) != 0 {
		return "", errors.New("pwd takes no argument")
	}
	return s.remoteWD, nil
}

func (s *shellstate) lpwd(args []string) (string, error) {
	if len(args) != 0 {
		return "", errors.New("lpwd takes no argument")
	}
	return s.localWD, nil
}

func (s *shellstate) lcd(args []string) (string, error) {
	if len(args) != 1 {
		return "", errors.New("lcd takes only one argument")
	}
	d := join(s.localWD, args[0])
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
	s.localWD = d
	return "", nil
}

func (s *shellstate) cd(args []string) (string, error) {
	if len(args) != 1 {
		return "", errors.New("cd takes only one argument")
	}
	d := join(s.remoteWD, args[0])
	stats, err := s.client.Stat(d)
	if err != nil {
		return "", err
	}
	if !stats.IsDir() {
		return "", errors.New("not a directory")
	}
	f, err := s.client.Open(d)
	_ = f.Close()
	if err != nil {
		return "", err
	}
	s.remoteWD = d
	return "", nil
}

func (s *shellstate) lls(args []string) (string, error) {
	c, err := os.Open(s.localWD)
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
	return formatListOfFiles(s.width(), files)
}

func (s *shellstate) ls(args []string) (string, error) {
	files, err := s.client.ReadDir(s.remoteWD)
	if err != nil {
		return "", fmt.Errorf("error listing directory: %s", err)
	}
	if len(files) == 0 {
		fmt.Println()
		return "", nil
	}
	return formatListOfFiles(s.width(), files)
}

func (s *shellstate) completeLess(args []string) []string {
	if len(args) > 1 {
		return nil
	}
	files, err := s.client.ReadDir(s.remoteWD)
	if err != nil {
		return nil
	}
	return completeFiles(args, files, false, true)
}

func (s *shellstate) completeLless(args []string) []string {
	if len(args) > 1 {
		return nil
	}
	c, err := os.Open(s.localWD)
	if err != nil {
		return nil
	}
	files, err := c.Readdir(0)
	_ = c.Close()
	if err != nil {
		return nil
	}

	return completeFiles(args, files, false, true)
}

func (s *shellstate) completeLcd(args []string) []string {
	if len(args) > 1 {
		return nil
	}
	c, err := os.Open(s.localWD)
	if err != nil {
		return nil
	}
	files, err := c.Readdir(0)
	_ = c.Close()
	if err != nil {
		return nil
	}
	return completeFiles(args, files, true, false)
}

func (s *shellstate) completeCd(args []string) []string {
	if len(args) > 1 {
		return nil
	}
	files, err := s.client.ReadDir(s.remoteWD)
	if err != nil {
		return nil
	}
	return completeFiles(args, files, true, false)
}

func completeFiles(args []string, files []os.FileInfo, onlyDirs, onlyFiles bool) []string {
	var props []string

	if onlyDirs {
		linq.From(files).
			WhereT(func(info os.FileInfo) bool { return info.IsDir() }).
			SelectT(func(info os.FileInfo) string { return info.Name() }).
			ToSlice(&props)
	} else if onlyFiles {
		linq.From(files).
			WhereT(func(info os.FileInfo) bool { return info.Mode().IsRegular() }).
			SelectT(func(info os.FileInfo) string { return info.Name() }).
			ToSlice(&props)
	} else {
		linq.From(files).
			SelectT(func(info os.FileInfo) string { return info.Name() }).
			ToSlice(&props)
	}
	if len(args) == 1 {
		linq.From(props).WhereT(func(s string) bool { return strings.HasPrefix(s, args[0]) }).ToSlice(&props)
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

func formatListOfFiles(width int, files []os.FileInfo) (string, error) {
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
	lines := make([][]interface{}, percolumn)
	var line int
	for _, f := range files {
		line++
		if line > percolumn {
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

func colorize(name, text string) string {
	lexer := lexers.Match(filepath.Base(name))
	if lexer == nil {
		lexer = lexers.Fallback
	}
	style := styles.Get("monokai")
	if style == nil {
		style = styles.Fallback
	}
	formatter := formatters.Get("terminal256")
	if formatter == nil {
		return text
	}
	iterator, err := lexer.Tokenise(nil, text)
	if err != nil {
		return text
	}
	var buf strings.Builder
	err = formatter.Format(&buf, style, iterator)
	if err != nil {
		return text
	}
	return tview.TranslateANSI(buf.String())
}
