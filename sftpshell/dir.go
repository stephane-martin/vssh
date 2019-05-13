package sftpshell

import (
	"errors"
	"github.com/mitchellh/go-homedir"
	"github.com/scylladb/go-set/strset"
	"os"
	"path/filepath"
	"strings"
)

func (s *ShellState) lcd(args []string, flags *strset.Set) error {
	var err error
	if len(args) > 1 {
		return errors.New("lcd takes only one argument")
	}
	if len(args) == 0 {
		name, err := homedir.Dir()
		if err != nil {
			return err
		}
		args = append(args, name)
	}
	dirname := join(s.LocalWD, strings.TrimRight(args[0], "/"))
	dirname, err = filepath.EvalSymlinks(dirname)
	if err != nil {
		return err
	}
	stats, err := os.Stat(dirname)
	if err != nil {
		return err
	}
	if !stats.IsDir() {
		return errors.New("not a directory")
	}
	f, err := os.Open(dirname)
	if err != nil {
		return err
	}
	_ = f.Close()
	s.LocalWD = dirname
	return nil
}

func (s *ShellState) cd(args []string, flags *strset.Set) error {
	if len(args) > 1 {
		return errors.New("cd takes only one argument")
	}
	if len(args) == 0 {
		args = append(args, s.initRemoteWD)
	}
	dirname := join(s.RemoteWD, strings.TrimRight(args[0], "/"))
	stats, err := s.client.Stat(dirname)
	if err != nil {
		return err
	}
	if !stats.IsDir() {
		return errors.New("not a directory")
	}
	f, err := s.client.Open(dirname)
	if err != nil {
		return err
	}
	_ = f.Close()
	s.RemoteWD = dirname
	return nil
}


func (s *ShellState) mkdir(args []string, flags *strset.Set) (e error) {
	if len(args) == 0 {
		return errors.New("mkdir needs at least one argument")
	}
	for _, name := range args {
		path := join(s.RemoteWD, name)
		err := s.client.Mkdir(path)
		if err != nil {
			s.err("%s: %s", name, err)
			if e == nil {
				e = err
			}
		}
	}
	return e
}