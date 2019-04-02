package lib

import (
	"github.com/karrick/godirwalk"
	"os"
	"path/filepath"
	"strings"
)

type Entry struct {
	Path string
	RelName string
	IsDir bool
}

func Walk() ([]Entry, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return nil, err
	}
	paths := make([]Entry, 0)
	err = godirwalk.Walk(cwd, &godirwalk.Options{
		Callback: func(osPathname string, de *godirwalk.Dirent) error {
			if de.IsDir() && strings.HasPrefix(de.Name(), ".") {
				return filepath.SkipDir
			}
			if !de.IsDir() && strings.HasPrefix(de.Name(), ".") {
				return nil
			}
			relName, err := filepath.Rel(cwd, osPathname)
			if err != nil {
				return err
			}
			if relName == "." {
				return nil
			}
			paths = append(paths, Entry{
				Path: osPathname,
				RelName: relName,
				IsDir: de.IsDir(),
			})
			//fmt.Printf("%s %s\n", de.ModeType(), osPathname)
			return nil
		},
	})
	if err != nil {
		return nil, err
	}
	return paths, nil
}
