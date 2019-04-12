package main

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/ahmetb/go-linq"
	"github.com/mattn/go-shellwords"
	"github.com/mitchellh/go-homedir"
	"github.com/pkg/sftp"
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
	return "", lib.ShowFile(fname, f, s.externalPager)
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
	return "", lib.ShowFile(fname, f, s.externalPager)
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
	return lib.FormatListOfFiles(s.width(), false, files)
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
		selected, err := lib.TableOfFiles(s.LocalWD, files)
		if err != nil {
			return "", err
		}
		if selected.Name == "" {
			return "", nil
		}
		if selected.Name == ".." {
			_, err := s.lcd([]string{".."})
			if err != nil {
				return "", err
			}
		} else if selected.Mode.IsDir() {
			_, err := s.lcd([]string{selected.Name})
			if err != nil {
				return "", err
			}
		} else {
			_, err := s.lless([]string{selected.Name})
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
		selected, err := lib.TableOfFiles(s.RemoteWD, files)
		if err != nil {
			return "", err
		}
		if selected.Name == "" {
			return "", nil
		}
		if selected.Name == ".." {
			_, err := s.cd([]string{".."})
			if err != nil {
				return "", err
			}
		} else if selected.Mode.IsDir() {
			_, err := s.cd([]string{selected.Name})
			if err != nil {
				return "", err
			}
		} else {
			_, err := s.less([]string{selected.Name})
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
	return lib.FormatListOfFiles(s.width(), false, files)
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
