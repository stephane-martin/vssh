package sftpshell

import (
	"fmt"
	cowsay "github.com/Code-Hex/Neo-cowsay"
	"github.com/pkg/sftp"
	"github.com/scylladb/go-set/strset"
	"github.com/stephane-martin/vssh/format"
	"github.com/stephane-martin/vssh/remoteops"
	"github.com/stephane-martin/vssh/sys"
	"github.com/stephane-martin/vssh/widgets"
	"io"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

func _ls(wd string, width int, args []string, flags *strset.Set, client *sftp.Client, out io.Writer) error {
	// TODO: -d, -R, -S, -X
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
		stats := make([]sys.UFile, 0, f.Size())
		names := f.List()
		sort.Strings(names)
		for _, fname := range names {
			if showHidden || !strings.HasPrefix(fname, ".") {
				s, err := stat(join(join(wd, d), fname))
				if err != nil {
					continue
				}
				stats = append(stats, sys.UFile{FileInfo: s, Path: fname})
			}
		}
		format.ListOfFiles(width, flags.Has("l"), stats, out)
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

func (s *ShellState) lls(args []string, flags *strset.Set) error {
	return _ls(s.LocalWD, s.Width(), args, flags, nil, s.out)
}

func (s *ShellState) ls(args []string, flags *strset.Set) error {
	return _ls(s.RemoteWD, s.Width(), args, flags, s.client, s.out)
}

func (s *ShellState) lll(args []string, flags *strset.Set) error {
	callback := func(f *widgets.SelectedFile) ([]os.FileInfo, error) {
		if f == nil || f.Action == widgets.Init {
			files, err := ioutil.ReadDir(s.LocalWD)
			if err != nil {
				return nil, err
			}
			return files, nil

		}
		if f.Action == widgets.OpenDir {
			err := s.lcd([]string{f.Name}, strset.New())
			if err != nil {
				return nil, err
			}
			files, err := ioutil.ReadDir(s.LocalWD)
			if err != nil {
				return nil, err
			}
			return files, nil
		}
		if f.Action == widgets.OpenFile {
			return nil, s.lopen([]string{f.Name}, strset.New())
		}
		if f.Action == widgets.DeleteFile {
			err := s.lrm([]string{f.Name}, strset.New())
			if err != nil {
				return nil, err
			}
			files, err := ioutil.ReadDir(s.LocalWD)
			if err != nil {
				return nil, err
			}
			return files, nil
		}
		if f.Action == widgets.DeleteDir {
			err := s.lrmdir([]string{f.Name}, strset.New())
			if err != nil {
				return nil, err
			}
			files, err := ioutil.ReadDir(s.LocalWD)
			if err != nil {
				return nil, err
			}
			return files, nil
		}

		if f.Action == widgets.EditFile {
			err := s.ledit([]string{f.Name}, strset.New())
			if err != nil {
				return nil, err
			}
			files, err := ioutil.ReadDir(s.LocalWD)
			if err != nil {
				return nil, err
			}
			return files, nil
		}

		return nil, fmt.Errorf("unknown action: %d", f.Action)
	}
	readFile := func(fname string) ([]byte, error) {
		fname = join(s.LocalWD, fname)
		content, err := ioutil.ReadFile(fname)
		if err != nil {
			return nil, err
		}
		return content, nil
	}
	s.report = false
	err := widgets.TableOfFiles(s.LocalWD, callback, readFile, false)
	s.report = true
	if err == widgets.ErrSwitch {
		return s.ll(args, flags)
	}
	return err
}

func (s *ShellState) ll(args []string, flags *strset.Set) error {
	callback := func(f *widgets.SelectedFile) ([]os.FileInfo, error) {
		if f == nil || f.Action == widgets.Init {
			files, err := s.client.ReadDir(s.RemoteWD)
			if err != nil {
				return nil, err
			}
			return files, nil

		}
		if f.Action == widgets.OpenDir {
			err := s.cd([]string{f.Name}, strset.New())
			if err != nil {
				return nil, err
			}
			files, err := s.client.ReadDir(s.RemoteWD)
			if err != nil {
				return nil, err
			}
			return files, nil
		}
		if f.Action == widgets.OpenFile {
			return nil, s.open([]string{f.Name}, strset.New())
		}
		if f.Action == widgets.DeleteFile {
			err := s.rm([]string{f.Name}, strset.New())
			if err != nil {
				return nil, err
			}
			files, err := s.client.ReadDir(s.LocalWD)
			if err != nil {
				return nil, err
			}
			return files, nil
		}
		if f.Action == widgets.DeleteDir {
			err := s.rmdir([]string{f.Name}, strset.New())
			if err != nil {
				return nil, err
			}
			files, err := s.client.ReadDir(s.LocalWD)
			if err != nil {
				return nil, err
			}
			return files, nil
		}

		if f.Action == widgets.EditFile {
			err := s.edit([]string{f.Name}, strset.New())
			if err != nil {
				return nil, err
			}
			files, err := s.client.ReadDir(s.LocalWD)
			if err != nil {
				return nil, err
			}
			return files, nil
		}

		return nil, fmt.Errorf("unknown action: %d", f.Action)
	}

	readFile := func(fname string) ([]byte, error) {
		fname = join(s.RemoteWD, fname)
		f, err := s.client.Open(fname)
		if err != nil {
			return nil, err
		}
		content, err := ioutil.ReadAll(f)
		_ = f.Close()
		if err != nil {
			return nil, err
		}
		return content, nil
	}

	s.report = false
	err := widgets.TableOfFiles(s.RemoteWD, callback, readFile, true)
	s.report = true
	if err == widgets.ErrSwitch {
		return s.lll(args, flags)
	}
	return err
}

func (s *ShellState) cowsay(args []string, flags *strset.Set) error {
	say := "MOO"
	if len(args) > 0 {
		say = args[0]
	} else {
		c := exec.Command("fortune", "-s", "-u", "-a")
		out, err := c.Output()
		if err == nil {
			say = string(out)
		}
	}
	r, err := cowsay.Say(
		cowsay.Phrase(say),
	)
	if err != nil {
		return err
	}
	fmt.Fprintln(s.out, r)
	fmt.Fprintln(s.out)
	return nil
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
		return remoteops.WalkLocal(wd, cb, nil)
	}
	return remoteops.WalkRemote(client, wd, cb, nil)
}

func (s *ShellState) list(args []string, flags *strset.Set) error {
	return _list(s.RemoteWD, s.client, flags, s.out)
}

func (s *ShellState) llist(args []string, flags *strset.Set) error {
	return _list(s.LocalWD, nil, flags, s.out)
}