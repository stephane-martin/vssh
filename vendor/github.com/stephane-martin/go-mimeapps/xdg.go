package mimeapps

import (
	"os"
	"path/filepath"
)

type XDGDirs struct {
	ConfigHome string
	DataHome   string
	ConfigDirs []string
	DataDirs   []string
}

func XDG() (dirs XDGDirs, err error) {
	dirs.ConfigHome = os.Getenv("XDG_CONFIG_HOME")
	dirs.DataHome = os.Getenv("XDG_DATA_HOME")
	configDirs := os.Getenv("XDG_CONFIG_DIRS")
	if configDirs != "" {
		dirs.ConfigDirs = filepath.SplitList(configDirs)
	}
	dataDirs := os.Getenv("XDG_DATA_DIRS")
	if dataDirs != "" {
		dirs.DataDirs = filepath.SplitList(dataDirs)
	}
	if dirs.ConfigHome != "" && dirs.DataHome != "" && len(dirs.DataDirs) > 0 && len(dirs.ConfigDirs) > 0 {
		return dirs, nil
	}
	home, err := DirHome()
	if err != nil {
		return dirs, err
	}
	if dirs.ConfigHome == "" {
		dirs.ConfigHome = filepath.Join(home, ".config")
	}
	if dirs.DataHome == "" {
		dirs.DataHome = filepath.Join(home, ".local", "share")
	}

	if len(dirs.ConfigDirs) == 0 {
		dirs.ConfigDirs = []string{"/etc/xdg"}
	}

	if len(dirs.DataDirs) == 0 {
		dirs.DataDirs = []string{"/usr/local/share", "/usr/share"}
	}
	return dirs, nil
}

func DefaultsPathsList() ([]string, error) {
	dirs, err := XDG()
	if err != nil {
		return nil, err
	}
	var defaults, dirnames []string
	dirnames = append(dirnames, dirs.DataHome)
	dirnames = append(dirnames, dirs.DataDirs...)
	for _, dirname := range dirnames {
		dirname = filepath.Join(dirname, "applications")
		if DirExists(dirname) {
			filename := filepath.Join(dirname, "defaults.list")
			if FileExists(filename) {
				defaults = append(defaults, filename)
			}
			filename = filepath.Join(dirname, "mimeinfo.cache")
			if FileExists(filename) {
				defaults = append(defaults, filename)
			}

		}
	}
	return defaults, nil
}

func MimeAppsPathsList() ([]string, error) {
	dirs, err := XDG()
	if err != nil {
		return nil, err
	}
	var paths []string
	var dirnames []string
	dirnames = append(dirnames, dirs.ConfigHome)
	dirnames = append(dirnames, dirs.ConfigDirs...)
	for _, dirname := range dirnames {
		if DirExists(dirname) {
			filename := filepath.Join(dirname, "mimeapps.list")
			if FileExists(filename) {
				paths = append(paths, filename)
			}
		}
	}
	dirnames = dirnames[0:0]
	dirnames = append(dirnames, dirs.DataHome)
	dirnames = append(dirnames, dirs.DataDirs...)
	for _, dirname := range dirnames {
		dirname = filepath.Join(dirname, "applications")
		if DirExists(dirname) {
			filename := filepath.Join(dirname, "mimeapps.list")
			if FileExists(filename) {
				paths = append(paths, filename)
			}
		}
	}
	return paths, nil
}

func DirExists(path string) bool {
	stats, err := os.Stat(path)
	if err != nil {
		return false
	}
	if !stats.IsDir() {
		return false
	}
	return true
}

func FileExists(path string) bool {
	stats, err := os.Stat(path)
	if err != nil {
		return false
	}
	if !stats.Mode().IsRegular() {
		return false
	}
	return true
}
