package sftpshell

import (
	"errors"
	"github.com/scylladb/go-set/strset"
	"github.com/stephane-martin/go-mimeapps"
	"github.com/stephane-martin/vssh/widgets"
	"io/ioutil"
	"os"
)

func (s *ShellState) less(args []string, flags *strset.Set) error {
	if len(args) != 1 {
		return errors.New("less takes one argument")
	}
	fname := join(s.RemoteWD, args[0])
	f, err := s.client.Open(fname)
	if err != nil {
		return err
	}
	content, err := ioutil.ReadAll(f)
	_ = f.Close()
	if err != nil {
		return err
	}
	return widgets.ShowFile(fname, content, s.externalPager)
}

func (s *ShellState) lless(args []string, flags *strset.Set) error {
	if len(args) != 1 {
		return errors.New("less takes one argument")
	}
	fname := join(s.LocalWD, args[0])
	content, err := ioutil.ReadFile(fname)
	if err != nil {
		return err
	}
	return widgets.ShowFile(fname, content, s.externalPager)
}


func (s *ShellState) lopen(args []string, flags *strset.Set) error {
	if len(args) != 1 {
		return errors.New("lopen takes exactly one argument")
	}
	filename := join(s.LocalWD, args[0])
	f, err := os.Open(filename)
	if err != nil {
		return err
	}
	tempFile, err := mimeapps.OpenRemote(filename, f)
	_ = f.Close()
	if tempFile != "" {
		s.tempfiles.Add(tempFile)
	}
	return err
}

func (s *ShellState) open(args []string, flags *strset.Set) error {
	if len(args) != 1 {
		return errors.New("open takes exactly one argument")
	}
	filename := join(s.RemoteWD, args[0])
	f, err := s.client.Open(filename)
	if err != nil {
		return err
	}
	tempFile, err := mimeapps.OpenRemote(filename, f)
	_ = f.Close()
	if tempFile != "" {
		s.tempfiles.Add(tempFile)
	}
	return err
}