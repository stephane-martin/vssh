package remoteops

import (
	"github.com/ktr0731/go-fuzzyfinder"
	"github.com/pkg/sftp"
	"github.com/stephane-martin/vssh/c"
	"go.uber.org/zap"
	"path/filepath"
	"strings"
)

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
			return c.FolderIcon + paths[i].rel
		}
		return c.FileIcon + paths[i].rel
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

func Fuzzy(client *sftp.Client, wd string, l *zap.SugaredLogger) ([]string, error) {
	if client == nil {
		return FuzzyLocal(wd, l)
	}
	return FuzzyRemote(client, wd, nil)
}

