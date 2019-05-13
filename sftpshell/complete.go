package sftpshell

import (
	"github.com/pkg/sftp"
	"github.com/stephane-martin/vssh/functional"
	"github.com/stephane-martin/vssh/remoteops"
	"github.com/stephane-martin/vssh/shell"
	"io/ioutil"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

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

func _completeArgManyDirs(wd string, client *sftp.Client, args []string, lastSpace bool) []string {
	if len(args) == 0 {
		return _completeArgOne(wd, client, nil, false, onlyDirs)
	}
	var arg, firstArgs []string
	if lastSpace {
		firstArgs = args
	} else {
		arg = args[len(args)-1:]
		firstArgs = args[0 : len(args)-1]
	}
	shell.QuoteSlice(firstArgs)
	if !lastSpace && remoteops.HasMeta(arg[0]) {
		// expand the glob pattern
		matches, err := findMatches(arg, wd, client, onlyDirs)
		if err != nil {
			return nil
		}
		if matches.Size() == 0 {
			return []string{functional.JoinSlices(" ", firstArgs)}
		}
		list := make([]string, 0, matches.Size())
		matches.Each(func(m string) bool {
			list = append(list, shell.QuoteString(rel(wd, m)))
			return true
		})
		sort.Strings(list)
		return []string{functional.JoinSlices(" ", firstArgs, list) + " "}
	}
	var props []string
	if lastSpace {
		// new last empty argument
		props = _completeArgOne(wd, client, nil, false, onlyDirs)
	} else {
		// there is no glob pattern: try to complete the last argument
		props = _completeArgOne(wd, client, arg, false, onlyDirs)
	}
	if len(props) == 0 {
		return nil
	}
	if len(firstArgs) == 0 {
		return props
	}
	functional.MapSlice(props, func(s string) string {
		return strings.Join(firstArgs, " ") + " " + s
	})
	return props

}

func _completeArgManyFile(wd string, client *sftp.Client, args []string, lastSpace bool) []string {
	if len(args) == 0 {
		return _completeArgOne(wd, client, nil, false, filesAndDirs)
	}
	var arg, firstArgs []string
	if lastSpace {
		firstArgs = args
	} else {
		arg = args[len(args)-1:]
		firstArgs = args[0 : len(args)-1]
	}
	shell.QuoteSlice(firstArgs)
	if !lastSpace && remoteops.HasMeta(arg[0]) {
		// expand the glob pattern
		matches, err := findMatches(arg, wd, client, onlyFiles)
		if err != nil {
			return nil
		}
		if matches.Size() == 0 {
			return []string{functional.JoinSlices(" ", firstArgs)}
		}
		list := make([]string, 0, matches.Size())
		matches.Each(func(m string) bool {
			list = append(list, shell.QuoteString(rel(wd, m)))
			return true
		})
		sort.Strings(list)
		return []string{functional.JoinSlices(" ", firstArgs, list) + " "}
	}
	var props []string
	if lastSpace {
		// new last empty argument
		props = _completeArgOne(wd, client, nil, false, filesAndDirs)
	} else {
		// there is no glob pattern: try to complete the last argument
		props = _completeArgOne(wd, client, arg, false, filesAndDirs)
	}
	if len(props) == 0 {
		return nil
	}
	if len(firstArgs) == 0 {
		return props
	}
	functional.MapSlice(props, func(s string) string {
		return strings.Join(firstArgs, " ") + " " + s
	})
	return props
}

func _completeArgOne(wd string, client *sftp.Client, args []string, lastSpace bool, o only) []string {
	if lastSpace || len(args) > 1 {
		return nil
	}
	readDir := ioutil.ReadDir
	stat := os.Stat
	if client != nil {
		readDir = client.ReadDir
		stat = client.Stat
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
	// replace symbolic links entries returned by readDir
	filtered := files[0:0]
	for i := range files {
		if !isLink(files[i]) {
			filtered = append(filtered, files[i])
			continue
		}
		stats, err := stat(join(dirname, files[i].Name()))
		if err != nil {
			continue
		}
		filtered = append(filtered, stats)
	}
	files = filtered
	props := completeFiles(cand, files, o)
	if len(props) == 0 {
		return nil
	}
	firstProp := props[0]
	for i := range props {
		props[i] = shell.QuoteString(join(relDirname, props[i]))
	}
	sort.Strings(props)
	if len(props) == 1 && !strings.HasSuffix(firstProp, "/") {
		props[0] = props[0] + " "
	}
	return props
}

func (s *ShellState) completeLedit(args []string, lastSpace bool) []string {
	return _completeArgManyFile(s.LocalWD, nil, args, lastSpace)
}

func (s *ShellState) completeEdit(args []string, lastSpace bool) []string {
	return _completeArgManyFile(s.RemoteWD, s.client, args, lastSpace)
}

func (s *ShellState) completeLrm(args []string, lastSpace bool) []string {
	return _completeArgManyFile(s.LocalWD, nil, args, lastSpace)
}

func (s *ShellState) completeRm(args []string, lastSpace bool) []string {
	return _completeArgManyFile(s.RemoteWD, s.client, args, lastSpace)
}

func (s *ShellState) completePut(args []string, lastSpace bool) []string {
	return _completeArgManyFile(s.LocalWD, nil, args, lastSpace)
}

func (s *ShellState) completeGet(args []string, lastSpace bool) []string {
	return _completeArgManyFile(s.RemoteWD, s.client, args, lastSpace)
}

func (s *ShellState) completeLopen(args []string, lastSpace bool) []string {
	return _completeArgOne(s.LocalWD, nil, args, lastSpace, filesAndDirs)
}

func (s *ShellState) completeOpen(args []string, lastSpace bool) []string {
	return _completeArgOne(s.RemoteWD, s.client, args, lastSpace, filesAndDirs)
}

func (s *ShellState) completeLless(args []string, lastSpace bool) []string {
	return _completeArgOne(s.LocalWD, nil, args, lastSpace, filesAndDirs)
}

func (s *ShellState) completeLess(args []string, lastSpace bool) []string {
	return _completeArgOne(s.RemoteWD, s.client, args, lastSpace, filesAndDirs)
}

func (s *ShellState) completeLcd(args []string, lastSpace bool) []string {
	return _completeArgOne(s.LocalWD, nil, args, lastSpace, onlyDirs)
}

func (s *ShellState) completeCd(args []string, lastSpace bool) []string {
	return _completeArgOne(s.RemoteWD, s.client, args, lastSpace, onlyDirs)
}

func (s *ShellState) completeLrmdir(args []string, lastSpace bool) []string {
	return _completeArgManyDirs(s.LocalWD, nil, args, lastSpace)
	//return _completeArgOne(s.LocalWD, ioutil.ReadDir, args, lastSpace, onlyDirs)
}

func (s *ShellState) completeRmdir(args []string, lastSpace bool) []string {
	return _completeArgManyDirs(s.RemoteWD, s.client, args, lastSpace)
	//return _completeArgOne(s.RemoteWD, s.client.ReadDir, args, lastSpace, onlyDirs)
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
			if isRegularOrLink(info) {
				props = append(props, info.Name())
			}
		}
	} else {
		for _, info := range files {
			if info.IsDir() {
				props = append(props, info.Name()+"/")
			} else if isRegularOrLink(info) {
				props = append(props, info.Name())
			}
		}
	}
	if candidate == "" {
		// filter out hidden files
		props = functional.FilterSlice(props, func(s string) bool {
			return !strings.HasPrefix(s, ".")
		})
	} else {
		props = functional.FilterSlice(props, func(s string) bool {
			return strings.HasPrefix(s, candidate)
		})
	}
	return props
}
