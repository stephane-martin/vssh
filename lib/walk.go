package lib

import (
	"path/filepath"
	"strings"

	"github.com/karrick/godirwalk"
	"github.com/ktr0731/go-fuzzyfinder"
	"go.uber.org/zap"
)

const folderIcon = "\xF0\x9F\x97\x80 "
const fileIcon = "\xF0\x9F\x97\x88 "

type entry struct {
	path  string
	rel   string
	isdir bool
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

func FuzzyLocal(wd string, logger *zap.SugaredLogger) ([]string, error) {
	var names []string
	var paths []entry
	err := WalkLocal(wd, func(path, rel string, isdir bool) error {
		if strings.HasPrefix(rel, ".") {
			if isdir {
				return filepath.SkipDir
			}
			return nil
		}
		paths = append(paths, entry{path: path, rel: rel, isdir: isdir})
		return nil
	}, logger)
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
