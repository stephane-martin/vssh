package main

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/gdamore/tcell"
	"github.com/mattn/go-shellwords"
	"github.com/mitchellh/go-homedir"
	"github.com/pkg/sftp"
	"github.com/rivo/tview"
	"github.com/scylladb/go-set/strset"
	"github.com/stephane-martin/go-mimeapps"
	"github.com/stephane-martin/vssh/lib"
	"golang.org/x/crypto/ssh/terminal"
	"golang.org/x/sync/errgroup"
)

type only int

const (
	filesAndDirs only = iota
	onlyFiles
	onlyDirs
)

type command func([]string, *strset.Set) error

type cmpl func([]string, bool) []string

type shellstate struct {
	LocalWD       string
	RemoteWD      string
	initRemoteWD  string
	client        *sftp.Client
	methods       map[string]command
	completes     map[string]cmpl
	externalPager bool
	tempfiles     *strset.Set
	info          func(string, ...interface{})
	err           func(string, ...interface{})
	out           io.Writer
}

func newShellState(client *sftp.Client, externalPager bool, out io.Writer, infoFunc func(string, ...interface{}), errFunc func(string, ...interface{})) (*shellstate, error) {
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
		tempfiles:     strset.New(),
		info:          infoFunc,
		err:           errFunc,
		out:           out,
	}
	s.methods = map[string]command{
		"less":      s.less,
		"lless":     s.lless,
		"ls":        s.ls,
		"lls":       s.lls,
		"ll":        s.ll,
		"lll":       s.lll,
		"llist":     s.llist,
		"list":      s.list,
		"cd":        s.cd,
		"lcd":       s.lcd,
		"edit":      s.edit,
		"ledit":     s.ledit,
		"open":      s.open,
		"lopen":     s.lopen,
		"exit":      s.exit,
		"logout":    s.exit,
		"q":         s.exit,
		":q":        s.exit,
		"pwd":       s.pwd,
		"lpwd":      s.lpwd,
		"get":       s.get,
		"put":       s.put,
		"mkdir":     s.mkdir,
		"mkdirall":  s.mkdirall,
		"lmkdir":    s.lmkdir,
		"lmkdirall": s.lmkdirall,
		"rm":        s.rm,
		"lrm":       s.lrm,
		"rmdir":     s.rmdir,
		"lrmdir":    s.lrmdir,
		"rename":    s.rename,
		"browse":    s.browse,
	}
	s.completes = map[string]cmpl{
		"cd":     s.completeCd,
		"lcd":    s.completeLcd,
		"less":   s.completeLess,
		"lless":  s.completeLless,
		"open":   s.completeOpen,
		"lopen":  s.completeLopen,
		"rmdir":  s.completeRmdir,
		"lrmdir": s.completeLrmdir,
		"ledit":  s.completeLedit,
		"edit":   s.completeEdit,
	}
	return s, nil
}

func (s *shellstate) Close() error {
	var err error
	s.tempfiles.Each(func(fname string) bool {
		e := os.Remove(fname)
		if e != nil && err == nil {
			err = e
		}
		return true
	})
	return err
}

func (s *shellstate) Width() int {
	width, _, err := terminal.GetSize(int(os.Stdout.Fd()))
	if err != nil {
		return 80
	}
	return width
}

func (s *shellstate) exit(_ []string, flags *strset.Set) error {
	return io.EOF
}

func (s *shellstate) Complete(cmd string, args []string, lastSpace bool) []string {
	fun := s.completes[cmd]
	if fun == nil {
		return nil
	}
	return fun(args, lastSpace)
}

func (s *shellstate) Dispatch(line string) error {
	line = strings.TrimSpace(line)
	if line == "" {
		return nil
	}

	p := shellwords.NewParser()
	args, err := p.Parse(line)
	if err != nil {
		return err
	}
	if p.Position != -1 {
		return errors.New("incomplete parsing error")
	}
	cmd := strings.ToLower(args[0])
	var posargs []string
	sflags := strset.New()
	for _, s := range args[1:] {
		if strings.HasPrefix(s, "-") {
			sflags.Add(strings.TrimLeft(s, "-"))
		} else {
			posargs = append(posargs, s)
		}
	}
	fun := s.methods[cmd]
	if fun == nil {
		return fmt.Errorf("unknown command: %s", cmd)
	}
	return fun(posargs, sflags)
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

func _list(wd string, client *sftp.Client, flags *strset.Set, out io.Writer) error {
	showHidden := flags.Has("a")
	cb := func(path, relName string, isdir bool) error {
		isHidden := strings.HasPrefix(relName, ".")
		if isHidden && isdir && !showHidden {
			return filepath.SkipDir
		}
		if isHidden && !showHidden {
			return nil
		}
		if isdir {
			fmt.Fprint(out, relName+"/")
		} else {
			fmt.Fprint(out, relName)
		}
		fmt.Fprintln(out)
		return nil
	}
	if client == nil {
		return lib.WalkLocal(wd, cb, nil)
	}
	return lib.WalkRemote(client, wd, cb, nil)
}

func (s *shellstate) list(args []string, flags *strset.Set) error {
	return _list(s.RemoteWD, s.client, flags, s.out)
}

func (s *shellstate) llist(args []string, flags *strset.Set) error {
	return _list(s.LocalWD, nil, flags, s.out)
}

func (s *shellstate) browse(args []string, flags *strset.Set) error {
	addr := "127.0.0.1:8080"
	if len(args) > 0 {
		_, _, err := net.SplitHostPort(args[0])
		if err != nil {
			return fmt.Errorf("failed to parse HTTP listen address: %s", err)
		}
		addr = args[0]
	}
	tv := tview.NewTextView()
	tv.SetBorder(true)
	tv.SetDynamicColors(true)
	tv.SetTitle(fmt.Sprintf(" browsing: %s ", s.RemoteWD))
	tv.SetTitleColor(tcell.ColorBlue)
	tv.SetBorderPadding(1, 1, 1, 1)
	app := tview.NewApplication().SetRoot(tv, true)
	ctx := context.Background()
	g, lctx := errgroup.WithContext(ctx)

	pr, pw := io.Pipe()
	r := bufio.NewReader(pr)

	g.Go(func() error {
		for {
			line, err := r.ReadBytes('\n')
			if len(line) > 0 {
				_, _ = tv.Write(line)
				app.Draw()
			}
			if err != nil {
				return err
			}
		}
	})

	g.Go(func() error {
		err := browseDir(lctx, s.client, addr, s.RemoteWD, pw)
		_ = pw.Close()
		return err
	})

	g.Go(func() error {
		err := app.Run()
		if err == nil {
			return context.Canceled
		}
		return err
	})

	g.Go(func() error {
		<-lctx.Done()
		app.Stop()
		return context.Canceled
	})

	err := g.Wait()
	if err == context.Canceled {
		return nil
	}
	return err
}

func (s *shellstate) rename(args []string, flags *strset.Set) error {
	if len(args) != 2 {
		return errors.New("rename takes two arguments")
	}
	from := join(s.RemoteWD, args[0])
	to := join(s.RemoteWD, args[1])
	return s.client.Rename(from, to)
}

func (s *shellstate) mkdir(args []string, flags *strset.Set) error {
	if len(args) == 0 {
		return errors.New("mkdir needs at least one argument")
	}
	for _, name := range args {
		path := join(s.RemoteWD, name)
		err := s.client.Mkdir(path)
		if err != nil {
			s.err("%s: %s", name, err)
		}
	}
	return nil
}

func (s *shellstate) rm(args []string, flags *strset.Set) error {
	if len(args) == 0 {
		return errors.New("rm needs at least one argument")
	}
	for _, name := range args {
		path := join(s.RemoteWD, name)
		err := s.client.Remove(path)
		if err != nil {
			s.err("%s: %s", name, err)
		}
	}
	return nil
}

func (s *shellstate) rmdir(args []string, flags *strset.Set) error {
	if len(args) == 0 {
		return errors.New("rmdir needs at least one argument")
	}
	var _rmdir func(string) error
	_rmdir = func(dirname string) (e error) {
		stats, err := s.client.Stat(dirname)
		if err != nil {
			return err
		}
		if !stats.IsDir() {
			return s.client.Remove(dirname)
		}
		files, err := s.client.ReadDir(dirname)
		if err != nil {
			return err
		}
		for _, file := range files {
			path := join(dirname, file.Name())
			if file.IsDir() {
				err := _rmdir(path)
				if err != nil {
					s.err("rmdir on %s: %s", path, err)
					if e == nil {
						e = err
					}
				}
			} else {
				err := s.client.Remove(path)
				if err != nil {
					s.err("rm on %s: %s", path, err)
					if e == nil {
						e = err
					}
				}
			}
		}
		if e != nil {
			return e
		}
		return s.client.Remove(dirname)
	}
	for _, name := range args {
		path := join(s.RemoteWD, name)
		err := _rmdir(path)
		if err != nil {
			s.err("%s: %s", name, err)
		}
	}
	return nil
}

func (s *shellstate) mkdirall(args []string, flags *strset.Set) error {
	if len(args) == 0 {
		return errors.New("mkdirall needs at least one argument")
	}
	for _, name := range args {
		path := join(s.RemoteWD, name)
		err := s.client.MkdirAll(path)
		if err != nil {
			s.err("%s: %s", name, err)
		}
	}
	return nil
}

func (s *shellstate) lmkdir(args []string, flags *strset.Set) error {
	if len(args) == 0 {
		return errors.New("lmkdir needs at least one argument")
	}
	for _, name := range args {
		path := join(s.LocalWD, name)
		err := os.Mkdir(path, 0755)
		if err != nil {
			s.err("%s: %s", name, err)
		}
	}
	return nil
}

func (s *shellstate) lrm(args []string, flags *strset.Set) error {
	if len(args) == 0 {
		return errors.New("lrm needs at least one argument")
	}
	for _, name := range args {
		path := join(s.LocalWD, name)
		err := os.Remove(path)
		if err != nil {
			s.err("%s: %s", name, err)
		}
	}
	return nil
}

func (s *shellstate) lrmdir(args []string, flags *strset.Set) error {
	if len(args) == 0 {
		return errors.New("lrmdir needs at least one argument")
	}
	for _, name := range args {
		path := join(s.LocalWD, name)
		err := os.RemoveAll(path)
		if err != nil {
			s.err("%s: %s", name, err)
		}
	}
	return nil
}

func (s *shellstate) lmkdirall(args []string, flags *strset.Set) error {
	if len(args) == 0 {
		return errors.New("lmkdirall needs at least one argument")
	}
	for _, name := range args {
		path := join(s.LocalWD, name)
		err := os.MkdirAll(path, 0755)
		if err != nil {
			s.err("%s: %s", name, err)
		}
	}
	return nil
}

func (s *shellstate) less(args []string, flags *strset.Set) error {
	if len(args) != 1 {
		return errors.New("less takes one argument")
	}
	fname := join(s.RemoteWD, args[0])
	f, err := s.client.Open(fname)
	if err != nil {
		return err
	}
	content, err := ioutil.ReadAll(f)
	_ = f.Close()
	if err != nil {
		return err
	}
	return lib.ShowFile(fname, content, s.externalPager)
}

func (s *shellstate) lless(args []string, flags *strset.Set) error {
	if len(args) != 1 {
		return errors.New("less takes one argument")
	}
	fname := join(s.LocalWD, args[0])
	content, err := ioutil.ReadFile(fname)
	if err != nil {
		return err
	}
	return lib.ShowFile(fname, content, s.externalPager)
}

func (s *shellstate) get(args []string, flags *strset.Set) error {
	remoteWD := s.RemoteWD
	if len(args) == 0 {
		names, err := lib.FuzzyRemote(s.client, remoteWD, nil)
		if err != nil {
			return err
		}
		if len(names) == 0 {
			return nil
		}
		args = names
	}
	var files, dirs []string
	for _, name := range args {
		path := join(remoteWD, name)
		stats, err := s.client.Stat(path)
		if err != nil {
			s.err("%s: %s", name, err)
			continue
		}
		if stats.IsDir() {
			dirs = append(dirs, path)
		} else if stats.Mode().IsRegular() {
			files = append(files, path)
		} else {
			s.err("not a regular file: %s", name)
		}
	}

	localWD := s.LocalWD
	for _, name := range dirs {
		err := s.getdir(localWD, name)
		if err != nil {
			s.err("download %s: %s", name, err)
		}
	}
	for _, name := range files {
		err := s.getfile(localWD, name)
		if err != nil {
			s.err("download %s: %s", name, err)
		}
	}
	return nil
}

func (s *shellstate) getfile(targetLocalDir, remoteFile string) error {
	source, err := s.client.Open(remoteFile)
	if err != nil {
		return err
	}
	defer func() { _ = source.Close() }()

	localFilename := join(targetLocalDir, base(remoteFile))
	dest, err := os.Create(localFilename)
	if err != nil {
		return err
	}
	defer func() { _ = dest.Close() }()
	_, err = io.Copy(dest, source)
	if err != nil {
		return err
	}
	s.info("downloaded: %s", remoteFile)
	return nil
}

func (s *shellstate) getdir(targetLocalDir, remoteDir string) error {
	files, err := s.client.ReadDir(remoteDir)
	if err != nil {
		return err
	}
	newDirname := join(targetLocalDir, base(remoteDir))
	err = os.Mkdir(newDirname, 0755)
	if err != nil && !os.IsExist(err) {
		return err
	}
	for _, f := range files {
		fname := join(remoteDir, f.Name())
		if f.IsDir() {
			err := s.getdir(newDirname, fname)
			if err != nil {
				s.err("download %s: %s", fname, err)
			}
		} else if f.Mode().IsRegular() {
			err := s.getfile(newDirname, fname)
			if err != nil {
				s.err("download %s: %s", fname, err)
			}
		}
	}
	s.info("downloaded: %s", remoteDir)
	return nil
}

func (s *shellstate) put(args []string, flags *strset.Set) error {
	localWD := s.LocalWD
	if len(args) == 0 {
		names, err := lib.FuzzyLocal(localWD, nil)
		if err != nil {
			return err
		}
		if len(names) == 0 {
			return nil
		}
		args = names
	}
	// check all files exist locally
	var files, dirs []string
	for _, name := range args {
		path := join(localWD, name)
		stats, err := os.Stat(path)
		if err != nil {
			s.err("%s: %s", name, err)
			continue
		}
		if stats.IsDir() {
			dirs = append(dirs, path)
		} else if stats.Mode().IsRegular() {
			files = append(files, path)
		} else {
			s.err("not a regular file: %s", name)
		}
	}
	remoteWD := s.RemoteWD
	for _, name := range dirs {
		err := s.putdir(remoteWD, name)
		if err != nil {
			s.err("upload %s: %s", name, err)
		}
	}
	for _, name := range files {
		err := s.putfile(remoteWD, name)
		if err != nil {
			s.err("upload %s: %s", name, err)
		}
	}
	return nil
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
	s.info("uploaded: %s", localFile)
	return nil
}

func (s *shellstate) putdir(targetRemoteDir, localDir string) error {
	files, err := ioutil.ReadDir(localDir)
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
				s.err("upload %s: %s", fname, err)
			}
		} else if f.Mode().IsRegular() {
			err := s.putfile(newDirname, fname)
			if err != nil {
				s.err("upload %s: %s", fname, err)
			}
		}
	}
	s.info("uploaded: %s", localDir)
	return nil
}

func (s *shellstate) pwd(args []string, flags *strset.Set) error {
	if len(args) != 0 {
		return errors.New("pwd takes no argument")
	}
	fmt.Fprintln(s.out, s.RemoteWD)
	return nil
}

func (s *shellstate) lpwd(args []string, flags *strset.Set) error {
	if len(args) != 0 {
		return errors.New("lpwd takes no argument")
	}
	fmt.Fprintln(s.out, s.LocalWD)
	return nil
}

func (s *shellstate) lcd(args []string, flags *strset.Set) error {
	if len(args) > 1 {
		return errors.New("lcd takes only one argument")
	}
	if len(args) == 0 {
		name, err := homedir.Dir()
		if err != nil {
			return err
		}
		args = append(args, name)
	}
	dirname := join(s.LocalWD, strings.TrimRight(args[0], "/"))
	stats, err := os.Stat(dirname)
	if err != nil {
		return err
	}
	if !stats.IsDir() {
		return errors.New("not a directory")
	}
	f, err := os.Open(dirname)
	if err != nil {
		return err
	}
	_ = f.Close()
	s.LocalWD = dirname
	return nil
}

func (s *shellstate) cd(args []string, flags *strset.Set) error {
	if len(args) > 1 {
		return errors.New("cd takes only one argument")
	}
	if len(args) == 0 {
		args = append(args, s.initRemoteWD)
	}
	dirname := join(s.RemoteWD, strings.TrimRight(args[0], "/"))
	stats, err := s.client.Stat(dirname)
	if err != nil {
		return err
	}
	if !stats.IsDir() {
		return errors.New("not a directory")
	}
	f, err := s.client.Open(dirname)
	if err != nil {
		return err
	}
	_ = f.Close()
	s.RemoteWD = dirname
	return nil
}

func findMatches(args []string, wd string, client *sftp.Client, o only) (*strset.Set, error) {
	allmatches := strset.New()
	if len(args) == 0 {
		return allmatches, nil
	}

	var glob func(string, string) ([]string, error)
	var stat func(string) (os.FileInfo, error)
	if client == nil {
		glob = lib.LocalGlob
		stat = os.Stat
	} else {
		glob = func(wd string, pattern string) ([]string, error) {
			return lib.SFTPGlob(wd, client, pattern)
		}
		stat = client.Stat
	}

	for _, pattern := range args {
		// list matching files
		matches, err := glob(wd, pattern)
		if err != nil {
			return nil, fmt.Errorf("invalid pattern %s: %s", pattern, err)
		}
		for _, match := range matches {
			match = join(wd, match)
			if o == filesAndDirs {
				allmatches.Add(match)
			} else {
				stats, err := stat(match)
				if err != nil {
					return nil, err
				}
				if o == onlyDirs && stats.IsDir() {
					allmatches.Add(match)
				} else if o == onlyFiles && stats.Mode().IsRegular() {
					allmatches.Add(match)
				}
			}
		}
	}
	return allmatches, nil
}

func (s *shellstate) lopen(args []string, flags *strset.Set) error {
	if len(args) != 1 {
		return errors.New("lopen takes exactly one argument")
	}
	filename := join(s.LocalWD, args[0])
	f, err := os.Open(filename)
	if err != nil {
		return err
	}
	tempFile, err := mimeapps.OpenRemote(filename, f)
	_ = f.Close()
	if tempFile != "" {
		s.tempfiles.Add(tempFile)
	}
	return err
}

func (s *shellstate) open(args []string, flags *strset.Set) error {
	if len(args) != 1 {
		return errors.New("open takes exactly one argument")
	}
	filename := join(s.RemoteWD, args[0])
	f, err := s.client.Open(filename)
	if err != nil {
		return err
	}
	tempFile, err := mimeapps.OpenRemote(filename, f)
	_ = f.Close()
	if tempFile != "" {
		s.tempfiles.Add(tempFile)
	}
	return err
}

func (s *shellstate) ledit(args []string, flags *strset.Set) error {
	editor := os.Getenv("EDITOR")
	if editor == "" {
		editor = "vim"
	}
	editorExe, err := exec.LookPath(editor)
	if err != nil {
		return err
	}
	allmatches := strset.New()
	if len(args) == 0 {
		files, err := lib.FuzzyLocal(s.LocalWD, nil)
		if err != nil {
			return err
		}
		if len(files) == 0 {
			return nil
		}
		allmatches.Add(files...)
	} else {
		var err error
		allmatches, err = findMatches(args, s.LocalWD, nil, onlyFiles)
		if err != nil {
			return err
		}
	}
	if allmatches.Size() == 0 {
		// TODO: create new file
		return nil
	}
	cmd := exec.Command(editorExe, allmatches.List()...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func hashLocalFile(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	h := sha256.New()
	_, err = io.Copy(h, f)
	if err != nil {
		return nil, err
	}
	return h.Sum(nil), nil
}

func (s *shellstate) edit(args []string, flags *strset.Set) error {
	editor := os.Getenv("EDITOR")
	if editor == "" {
		editor = "vim"
	}
	editorExe, err := exec.LookPath(editor)
	if err != nil {
		return err
	}
	allmatches := strset.New()
	if len(args) == 0 {
		files, err := lib.FuzzyRemote(s.client, s.RemoteWD, nil)
		if err != nil {
			return err
		}
		if len(files) == 0 {
			return nil
		}
		allmatches.Add(files...)
	} else {
		allmatches, err = findMatches(args, s.RemoteWD, s.client, onlyFiles)
		if err != nil {
			return err
		}
	}
	if allmatches.Size() == 0 {
		// TODO: create new file
		return errors.New("no match")
	}

	// remote file to edit => local filename
	tempFiles := make(map[string]string)
	initialHashes := make(map[string][]byte)

	copyTemp := func(match string) error {
		f, err := s.client.Open(match)
		if err != nil {
			return fmt.Errorf("failed to open remote file: %s", err)
		}
		defer func() { _ = f.Close() }()
		stats, err := f.Stat()
		if err != nil {
			return fmt.Errorf("failed to stat remote file: %s", err)
		}
		if stats.IsDir() || !stats.Mode().IsRegular() {
			return nil
		}
		// create a temp directory for each file to edit
		t, err := ioutil.TempDir("", "vssh-shell-edit")
		if err != nil {
			return fmt.Errorf("failed to make temp directory %s: %s", t, err)
		}
		dest := join(t, filepath.Base(f.Name()))
		destFile, err := os.Create(dest)
		if err != nil {
			return fmt.Errorf("failed to create local file %s: %s", dest, err)
		}
		defer func() { _ = destFile.Close() }()
		_, err = io.Copy(destFile, f)
		_ = destFile.Close()
		if err != nil {
			_ = os.RemoveAll(t)
			return fmt.Errorf("failed to copy remote file %s: %s", f.Name(), err)
		}
		err = os.Chmod(dest, stats.Mode().Perm()&0700)
		if err != nil {
			s.err("failed to chmod local copy %s: %s", dest, err)
		}
		h, err := hashLocalFile(dest)
		if err != nil {
			_ = os.RemoveAll(t)
			return fmt.Errorf("failed to hash local copy %s: %s", dest, err)
		}
		tempFiles[match] = dest
		initialHashes[match] = h
		return nil
	}

	for _, remoteFilename := range allmatches.List() {
		// copy remote files to temp directories
		err := copyTemp(remoteFilename)
		if err != nil {
			s.err("%s: %s", remoteFilename, err)
		}
	}
	if len(tempFiles) == 0 {
		return nil
	}
	tempFilesList := make([]string, 0, len(tempFiles))
	for _, tempFilename := range tempFiles {
		fname := tempFilename
		tempFilesList = append(tempFilesList, fname)
		defer func() {
			_ = os.RemoveAll(filepath.Dir(fname))
		}()
	}

	cmd := exec.Command(editorExe, tempFilesList...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	err = cmd.Run()
	if err != nil {
		return err
	}

	upload := func(remoteFilename, tempFilename string) error {
		local, err := os.Open(tempFilename)
		if err != nil {
			return err
		}
		defer func() { _ = local.Close() }()
		remote, err := s.client.Create(remoteFilename)
		if err != nil {
			return err
		}
		defer func() { _ = remote.Close() }()
		_, err = io.Copy(remote, local)
		return err
	}

	// copy back the modified files to the remote side if needed
	for remoteFilename, tempFilename := range tempFiles {
		previousHash := initialHashes[remoteFilename]
		newHash, err := hashLocalFile(tempFilename)
		if err != nil {
			return err
		}
		if !bytes.Equal(previousHash, newHash) {
			err := upload(remoteFilename, tempFilename)
			if err != nil {
				s.err("%s: %s", remoteFilename, err)
			} else {
				s.info("modified: %s", remoteFilename)
			}
		}

	}
	return nil
}

func _ls(wd string, width int, args []string, flags *strset.Set, client *sftp.Client, out io.Writer) error {
	var stat func(path string) (os.FileInfo, error)
	var readdir func(string) ([]os.FileInfo, error)
	if client == nil {
		stat = os.Stat
		readdir = ioutil.ReadDir
	} else {
		stat = client.Stat
		readdir = client.ReadDir
	}
	showHidden := flags.Has("a")

	allmatches := strset.New()
	if len(args) == 0 {
		// no arg ==> list all files in current directory
		allmatches.Add(wd)
	} else {
		var err error
		allmatches, err = findMatches(args, wd, client, filesAndDirs)
		if err != nil {
			return err
		}
	}

	// map of directory ==> files
	files := make(map[string]*strset.Set)
	files["."] = strset.New()

	allmatches.Each(func(match string) bool {
		relMatch := rel(wd, match)
		stats, err := stat(match)
		if err != nil {
			return true
		}
		if stats.IsDir() {
			entries, err := readdir(match)
			if err != nil {
				return true
			}
			if _, ok := files[relMatch]; !ok {
				files[relMatch] = strset.New()
			}
			for _, entry := range entries {
				files[relMatch].Add(entry.Name())
			}
		} else {
			files["."].Add(relMatch)
		}
		return true
	})

	printDirectory := func(d string, f *strset.Set) {
		if f.Size() == 0 {
			return
		}
		if d != "." {
			fmt.Fprintf(out, "%s:\n", d)
		}
		stats := make([]lib.Unixfile, 0, f.Size())
		names := f.List()
		sort.Strings(names)
		for _, fname := range names {
			if showHidden || !strings.HasPrefix(fname, ".") {
				s, err := stat(join(join(wd, d), fname))
				if err != nil {
					continue
				}
				stats = append(stats, lib.Unixfile{FileInfo: s, Path: fname})
			}
		}
		lib.FormatListOfFiles(width, flags.Has("l"), stats, out)
		fmt.Fprintln(out)
	}

	dirnames := make([]string, 0, len(files))
	for dirname := range files {
		dirnames = append(dirnames, dirname)
	}
	sort.Strings(dirnames)
	if f, ok := files["."]; ok {
		printDirectory(".", f)
	}
	for _, dirname := range dirnames {
		if dirname == "." {
			continue
		}
		printDirectory(dirname, files[dirname])
	}
	return nil
}

func (s *shellstate) lls(args []string, flags *strset.Set) error {
	return _ls(s.LocalWD, s.Width(), args, flags, nil, s.out)
}

func (s *shellstate) ls(args []string, flags *strset.Set) error {
	return _ls(s.RemoteWD, s.Width(), args, flags, s.client, s.out)
}

func (s *shellstate) lll(args []string, flags *strset.Set) error {
	for {
		files, err := ioutil.ReadDir(s.LocalWD)
		if err != nil {
			return err
		}
		selected, err := lib.TableOfFiles(s.LocalWD, files, false)
		if err != nil {
			return err
		}
		if selected.Name == "" {
			return nil
		}
		if selected.Name == ".." {
			err := s.lcd([]string{".."}, strset.New())
			if err != nil {
				return err
			}
		} else if selected.Mode.IsDir() {
			err := s.lcd([]string{selected.Name}, strset.New())
			if err != nil {
				return err
			}
		} else {
			err := s.lless([]string{selected.Name}, strset.New())
			if err != nil {
				return err
			}
		}
	}
}

func (s *shellstate) ll(args []string, flags *strset.Set) error {
	for {
		files, err := s.client.ReadDir(s.RemoteWD)
		if err != nil {
			return fmt.Errorf("error listing directory: %s", err)
		}
		selected, err := lib.TableOfFiles(s.RemoteWD, files, true)
		if err != nil {
			return err
		}
		if selected.Name == "" {
			return nil
		}
		if selected.Name == ".." {
			err := s.cd([]string{".."}, strset.New())
			if err != nil {
				return err
			}
		} else if selected.Mode.IsDir() {
			err := s.cd([]string{selected.Name}, strset.New())
			if err != nil {
				return err
			}
		} else {
			err := s.less([]string{selected.Name}, strset.New())
			if err != nil {
				return err
			}
		}
	}
}

func base(s string) string {
	s = filepath.Base(s)
	if s == "/" {
		return ""
	}
	return s
}

func rel(base, fname string) string {
	relName, err := filepath.Rel(base, fname)
	if err != nil {
		return fname
	}
	if strings.HasPrefix(relName, "..") {
		return fname
	}
	return relName
}

func candidate(wd, input string) (cand, dirname, relDirname string) {
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
	return cand, dirname, rel(wd, dirname)
}

func _completeArgManyFile(wd string, client *sftp.Client, readDir func(string) ([]os.FileInfo, error), args []string, lastSpace bool) []string {
	if len(args) == 0 {
		return _completeArgOne(wd, readDir, nil, false, filesAndDirs)
	}
	var arg, firstArgs []string
	if lastSpace {
		firstArgs = args
	} else {
		arg = args[len(args)-1:]
		firstArgs = args[0 : len(args)-1]
	}
	quoteSlice(firstArgs)
	if !lastSpace && lib.HasMeta(arg[0]) {
		// expand the glob pattern
		matches, err := findMatches(arg, wd, client, onlyFiles)
		if err != nil {
			return nil
		}
		if matches.Size() == 0 {
			return []string{joinSlices(firstArgs)}
		}
		list := make([]string, 0, matches.Size())
		matches.Each(func(m string) bool {
			list = append(list, quoteString(rel(wd, m)))
			return true
		})
		sort.Strings(list)
		return []string{joinSlices(firstArgs, list) + " "}
	}
	var props []string
	if lastSpace {
		// new last empty argument
		props = _completeArgOne(wd, readDir, nil, false, filesAndDirs)
	} else {
		// there is no glob pattern: try to complete the last argument
		props = _completeArgOne(wd, readDir, arg, false, filesAndDirs)
	}
	if len(props) == 0 {
		return nil
	}
	if len(firstArgs) == 0 {
		return props
	}
	mapSlice(props, func(s string) string {
		return strings.Join(firstArgs, " ") + " " + s
	})
	return props
}

func _completeArgOne(wd string, readDir func(string) ([]os.FileInfo, error), args []string, lastSpace bool, o only) []string {
	if lastSpace || len(args) > 1 {
		return nil
	}
	var input string
	if len(args) == 1 {
		input = args[0]
	}
	cand, dirname, relDirname := candidate(wd, input)
	files, err := readDir(dirname)
	if err != nil {
		return nil
	}
	props := completeFiles(cand, files, o)
	if len(props) == 0 {
		return nil
	}
	firstProp := props[0]
	for i := range props {
		props[i] = quoteString(join(relDirname, props[i]))
	}
	sort.Strings(props)
	if len(props) == 1 && !strings.HasSuffix(firstProp, "/") {
		props[0] = props[0] + " "
	}
	return props
}

func (s *shellstate) completeLedit(args []string, lastSpace bool) []string {
	return _completeArgManyFile(s.LocalWD, nil, ioutil.ReadDir, args, lastSpace)
}

func (s *shellstate) completeEdit(args []string, lastSpace bool) []string {
	return _completeArgManyFile(s.RemoteWD, s.client, s.client.ReadDir, args, lastSpace)
}

func (s *shellstate) completeLopen(args []string, lastSpace bool) []string {
	return _completeArgOne(s.LocalWD, ioutil.ReadDir, args, lastSpace, filesAndDirs)
}

func (s *shellstate) completeOpen(args []string, lastSpace bool) []string {
	return _completeArgOne(s.RemoteWD, s.client.ReadDir, args, lastSpace, filesAndDirs)
}

func (s *shellstate) completeLless(args []string, lastSpace bool) []string {
	return _completeArgOne(s.LocalWD, ioutil.ReadDir, args, lastSpace, filesAndDirs)
}

func (s *shellstate) completeLess(args []string, lastSpace bool) []string {
	return _completeArgOne(s.RemoteWD, s.client.ReadDir, args, lastSpace, filesAndDirs)
}

func (s *shellstate) completeLcd(args []string, lastSpace bool) []string {
	return _completeArgOne(s.LocalWD, ioutil.ReadDir, args, lastSpace, onlyDirs)
}

func (s *shellstate) completeCd(args []string, lastSpace bool) []string {
	return _completeArgOne(s.RemoteWD, s.client.ReadDir, args, lastSpace, onlyDirs)
}

func (s *shellstate) completeLrmdir(args []string, lastSpace bool) []string {
	// FIX (many)
	return _completeArgOne(s.LocalWD, ioutil.ReadDir, args, lastSpace, onlyDirs)
}

func (s *shellstate) completeRmdir(args []string, lastSpace bool) []string {
	// FIX (many)
	return _completeArgOne(s.RemoteWD, s.client.ReadDir, args, lastSpace, onlyDirs)
}

func completeFiles(candidate string, files []os.FileInfo, o only) []string {
	props := make([]string, 0, len(files))

	if o == onlyDirs {
		for _, info := range files {
			if info.IsDir() {
				props = append(props, info.Name()+"/")
			}
		}
	} else if o == onlyFiles {
		for _, info := range files {
			if info.Mode().IsRegular() {
				props = append(props, info.Name())
			}
		}
	} else {
		for _, info := range files {
			if info.IsDir() {
				props = append(props, info.Name()+"/")
			} else if info.Mode().IsRegular() {
				props = append(props, info.Name())
			}
		}
	}
	if candidate == "" {
		// filter out hidden files
		props = filterSlice(props, func(s string) bool {
			return !strings.HasPrefix(s, ".")
		})
	} else {
		props = filterSlice(props, func(s string) bool {
			return strings.HasPrefix(s, candidate)
		})
	}
	return props
}

func shorten(path string) string {
	d, f := filepath.Split(path)
	var buf strings.Builder
	var slash bool
	for _, c := range d {
		switch c {
		case '/':
			buf.WriteRune('/')
			slash = true
		default:
			if slash {
				buf.WriteRune(c)
			}
			slash = false
		}
	}
	buf.WriteString(f)
	return buf.String()
}
