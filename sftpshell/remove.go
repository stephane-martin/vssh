package sftpshell

import (
	"errors"
	"github.com/pkg/sftp"
	"github.com/scylladb/go-set/strset"
	"os"
)

func (s *ShellState) rm(args []string, flags *strset.Set) (e error) {
	if len(args) == 0 {
		return errors.New("rm needs at least one argument")
	}
	for _, name := range args {
		path := join(s.RemoteWD, name)
		err := s.client.Remove(path)
		if err != nil {
			s.err("%s: %s", name, err)
			if e == nil {
				e = err
			}
		}
	}
	return e
}

func _rmdir(client *sftp.Client, dirname string) (e error) {
	stats, err := client.Stat(dirname)
	if err != nil {
		return err
	}
	if !stats.IsDir() {
		return client.Remove(dirname)
	}
	files, err := client.ReadDir(dirname)
	if err != nil {
		return err
	}
	for _, file := range files {
		path := join(dirname, file.Name())
		if file.IsDir() {
			err := _rmdir(client, path)
			if err != nil {
				if e == nil {
					e = err
				}
			}
		} else {
			err := client.Remove(path)
			if err != nil {
				if e == nil {
					e = err
				}
			}
		}
	}
	if e != nil {
		return e
	}
	return client.Remove(dirname)

}

func (s *ShellState) rmdir(args []string, flags *strset.Set) (e error) {
	if len(args) == 0 {
		return errors.New("rmdir needs at least one argument")
	}
	for _, name := range args {
		path := join(s.RemoteWD, name)
		err := _rmdir(s.client, path)
		if err != nil {
			s.err("%s: %s", name, err)
			if e == nil {
				e = err
			}
		}
	}
	return e
}

func (s *ShellState) mkdirall(args []string, flags *strset.Set) (e error) {
	if len(args) == 0 {
		return errors.New("mkdirall needs at least one argument")
	}
	for _, name := range args {
		path := join(s.RemoteWD, name)
		err := s.client.MkdirAll(path)
		if err != nil {
			s.err("%s: %s", name, err)
			if e == nil {
				e = err
			}
		}
	}
	return e
}

func (s *ShellState) lmkdir(args []string, flags *strset.Set) (e error) {
	if len(args) == 0 {
		return errors.New("lmkdir needs at least one argument")
	}
	for _, name := range args {
		path := join(s.LocalWD, name)
		err := os.Mkdir(path, 0755)
		if err != nil {
			s.err("%s: %s", name, err)
			if e == nil {
				e = err
			}
		}
	}
	return e
}

func (s *ShellState) lrm(args []string, flags *strset.Set) (e error) {
	if len(args) == 0 {
		return errors.New("lrm needs at least one argument")
	}
	for _, name := range args {
		path := join(s.LocalWD, name)
		err := os.Remove(path)
		if err != nil {
			s.err("%s: %s", name, err)
			if e == nil {
				e = err
			}
		}
	}
	return e
}

func (s *ShellState) lrmdir(args []string, flags *strset.Set) (e error) {
	if len(args) == 0 {
		return errors.New("lrmdir needs at least one argument")
	}
	for _, name := range args {
		path := join(s.LocalWD, name)
		err := os.RemoveAll(path)
		if err != nil {
			s.err("%s: %s", name, err)
			if e == nil {
				e = err
			}
		}
	}
	return e
}

func (s *ShellState) lmkdirall(args []string, flags *strset.Set) (e error) {
	if len(args) == 0 {
		return errors.New("lmkdirall needs at least one argument")
	}
	for _, name := range args {
		path := join(s.LocalWD, name)
		err := os.MkdirAll(path, 0755)
		if err != nil {
			s.err("%s: %s", name, err)
			if e == nil {
				e = err
			}
		}
	}
	return e
}
