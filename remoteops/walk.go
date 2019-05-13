package remoteops

import (
	"github.com/karrick/godirwalk"
	"github.com/pkg/sftp"
	"go.uber.org/zap"
	"path/filepath"
)

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

func Walk(client *sftp.Client, wd string, cb ListCallback, l *zap.SugaredLogger) error {
	if client == nil {
		return WalkLocal(wd, cb, l)
	}
	return WalkRemote(client, wd, cb, l)
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

