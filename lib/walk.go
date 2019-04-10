package lib

import (
	"os"
	"path/filepath"

	"github.com/karrick/godirwalk"
	"go.uber.org/zap"
)

func WalkLocal(cb ListCallback, l *zap.SugaredLogger) error {
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	return godirwalk.Walk(cwd, &godirwalk.Options{
		Callback: func(osPathname string, de *godirwalk.Dirent) error {
			relName, err := filepath.Rel(cwd, osPathname)
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
			l.Debugw("error walking current directory", "path", path, "error", e)
			return godirwalk.SkipNode
		},
	})
}
