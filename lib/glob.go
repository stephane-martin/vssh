package lib

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/pkg/sftp"
)

var magicChars = `*?[`

func hasMeta(path string) bool {
	return strings.ContainsAny(path, magicChars)
}

func cleanGlobPath(path string) string {
	switch path {
	case "":
		return "."
	case string(filepath.Separator):
		// do nothing to the path
		return path
	default:
		return path[0 : len(path)-1] // chop off trailing separator
	}
}

type readDirNamesFunc func(path string) ([]string, error)

type lStatFunc func(string) (os.FileInfo, error)

func sftpReadDirNames(client *sftp.Client) readDirNamesFunc {
	return func(path string) ([]string, error) {
		fi, err := client.Stat(path)
		if err != nil {
			return nil, err
		}
		if !fi.IsDir() {
			return nil, fmt.Errorf("not a directory: %s", path)
		}
		files, err := client.ReadDir(path)
		if err != nil {
			return nil, err
		}
		names := make([]string, 0, len(files))
		for _, file := range files {
			names = append(names, file.Name())
		}
		sort.Strings(names)
		return names, nil
	}
}

func localReadDirNames(path string) ([]string, error) {
	fi, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if !fi.IsDir() {
		return nil, fmt.Errorf("not a directory: %s", path)
	}
	d, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	names, _ := d.Readdirnames(-1)
	_ = d.Close()
	sort.Strings(names)
	return names, nil
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

func glob(wd, dir, pattern string, matches []string, readDir readDirNamesFunc) (m []string, e error) {
	m = matches
	names, err := readDir(join(wd, dir))
	if err != nil {
		return
	}
	sort.Strings(names)

	for _, n := range names {
		if !strings.HasPrefix(n, ".") {
			matched, err := filepath.Match(pattern, n)
			if err != nil {
				return m, err
			}
			if matched {
				m = append(m, filepath.Join(dir, n))
			}
		}
	}
	return
}

func Glob(wd, pattern string, readDir readDirNamesFunc, lstat lStatFunc) (matches []string, err error) {
	if !hasMeta(pattern) {
		if _, err = lstat(join(wd, pattern)); err != nil {
			return nil, nil
		}
		return []string{pattern}, nil
	}

	dir, file := filepath.Split(pattern)
	dir = cleanGlobPath(dir)

	if !hasMeta(dir) {
		return glob(wd, dir, file, nil, readDir)
	}
	if dir == pattern {
		return nil, filepath.ErrBadPattern
	}

	var m []string
	m, err = Glob(wd, dir, readDir, lstat)
	if err != nil {
		return
	}
	for _, d := range m {
		matches, err = glob(wd, d, file, matches, readDir)
		if err != nil {
			return
		}
	}
	return
}

func SFTPGlob(wd string, client *sftp.Client, pattern string) ([]string, error) {
	return Glob(wd, pattern, sftpReadDirNames(client), client.Lstat)
}

func LocalGlob(wd, pattern string) ([]string, error) {
	return Glob(wd, pattern, localReadDirNames, os.Lstat)
}
