package lib

import (
	"path/filepath"
	"strings"

	"github.com/karrick/godirwalk"
	"github.com/ktr0731/go-fuzzyfinder"
	"github.com/pkg/sftp"
	"go.uber.org/zap"
)

const folderIcon = "\xF0\x9F\x97\x80 "
const fileIcon = "\xF0\x9F\x97\x88 "

type entry struct {
	path  string
	rel   string
	isdir bool
}

type ListCallback func(path, relName string, isDir bool) error
type walkFunc func(wd string, cb ListCallback, l *zap.SugaredLogger) error

func getWalkFunc(client *sftp.Client) walkFunc {
	if client == nil {
		return WalkLocal
	}
	return func(wd string, cb ListCallback, l *zap.SugaredLogger) error {
		return WalkRemote(client, wd, cb, l)
	}
}

func WalkLocal(wd string, cb ListCallback, l *zap.SugaredLogger) error {
	return godirwalk.Walk(wd, &godirwalk.Options{
		Callback: func(osPathname string, de *godirwalk.Dirent) error {
			relName, err := filepath.Rel(wd, osPathname)
			if err != nil {
				return err
			}
			if relName == "." {
				return nil
			}
			if de.IsDir() || de.IsRegular() {
				return cb(osPathname, relName, de.IsDir())
			}
			return nil
		},
		ErrorCallback: func(path string, e error) godirwalk.ErrorAction {
			if l != nil {
				l.Debugw("error walking current directory", "path", path, "error", e)
			}
			return godirwalk.SkipNode
		},
	})
}

func WalkRemote(client *sftp.Client, wd string, cb ListCallback, l *zap.SugaredLogger) error {
	walker := client.Walk(wd)
	for walker.Step() {
		osPathName := walker.Path() // p is in form wd/path
		relName, err := filepath.Rel(wd, osPathName)
		if err != nil {
			return err // should not happen
		}
		infos := walker.Stat()
		if walker.Err() != nil {
			if l != nil {
				l.Debugw("error walking current directory", "path", relName, "error", walker.Err())
			}
		} else if relName != "." && (infos.IsDir() || infos.Mode().IsRegular()) {
			err := cb(osPathName, relName, infos.IsDir())
			if err == filepath.SkipDir {
				walker.SkipDir()
			} else if err != nil {
				return err
			}
		}
	}
	return nil
}

func fuzzy(client *sftp.Client, wd string, l *zap.SugaredLogger) ([]string, error) {
	var names []string
	var paths []entry
	walk := getWalkFunc(client)
	err := walk(wd, func(path, rel string, isdir bool) error {
		if strings.HasPrefix(rel, ".") {
			if isdir {
				return filepath.SkipDir
			}
			return nil
		}
		paths = append(paths, entry{path: path, rel: rel, isdir: isdir})
		return nil
	}, l)
	if err != nil {
		return nil, err
	}
	idx, _ := fuzzyfinder.FindMulti(paths, func(i int) string {
		if paths[i].isdir {
			return folderIcon + paths[i].rel
		}
		return fileIcon + paths[i].rel
	})
	for _, i := range idx {
		names = append(names, paths[i].path)
	}
	return names, nil
}

func FuzzyLocal(wd string, l *zap.SugaredLogger) ([]string, error) {
	return fuzzy(nil, wd, l)
}

func FuzzyRemote(client *sftp.Client, wd string, l *zap.SugaredLogger) ([]string, error) {
	return fuzzy(client, wd, l)
}
