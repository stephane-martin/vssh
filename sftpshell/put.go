package sftpshell

import (
	"github.com/scylladb/go-set/strset"
	"github.com/stephane-martin/vssh/remoteops"
	"io"
	"io/ioutil"
	"os"
)

func (s *ShellState) put(args []string, flags *strset.Set) error {
	localWD := s.LocalWD
	if len(args) == 0 {
		names, err := remoteops.FuzzyLocal(localWD, nil)
		if err != nil {
			return err
		}
		if len(names) == 0 {
			return nil
		}
		args = names
	}
	// check all files exist locally
	var files, dirs []string
	for _, name := range args {
		path := join(localWD, name)
		stats, err := os.Stat(path)
		if err != nil {
			s.err("%s: %s", name, err)
			continue
		}
		if stats.IsDir() {
			dirs = append(dirs, path)
		} else if stats.Mode().IsRegular() {
			files = append(files, path)
		} else {
			s.err("not a regular file: %s", name)
		}
	}
	remoteWD := s.RemoteWD
	for _, name := range dirs {
		err := s.putdir(remoteWD, name)
		if err != nil {
			s.err("upload %s: %s", name, err)
		}
	}
	for _, name := range files {
		err := s.putfile(remoteWD, name)
		if err != nil {
			s.err("upload %s: %s", name, err)
		}
	}
	return nil
}

func (s *ShellState) putfile(targetRemoteDir string, localFile string) error {
	remoteFilename := join(targetRemoteDir, base(localFile))
	source, err := os.Open(localFile)
	if err != nil {
		return err
	}
	defer func() { _ = source.Close() }()
	stats, err := source.Stat()
	if err != nil {
		return err
	}
	dest, err := s.client.Create(remoteFilename)
	if err != nil {
		return err
	}
	defer func() { _ = dest.Close() }()
	s.info("uploading: %s", localFile)
	bar := newBar(stats.Size())
	_, err = io.Copy(dest, bar.NewProxyReader(source))
	bar.Finish()
	if err != nil {
		return err
	}
	s.info("uploaded: %s", localFile)
	return nil
}

func (s *ShellState) putdir(targetRemoteDir, localDir string) error {
	files, err := ioutil.ReadDir(localDir)
	if err != nil {
		return err
	}
	newDirname := join(targetRemoteDir, base(localDir))
	err = s.client.Mkdir(newDirname)
	if err != nil && !os.IsExist(err) {
		return err
	}
	s.info("upload: %s", localDir)

	for _, f := range files {
		fname := join(localDir, f.Name())
		if f.IsDir() {
			err := s.putdir(newDirname, fname)
			if err != nil {
				s.err("upload %s: %s", fname, err)
			}
		} else if f.Mode().IsRegular() {
			err := s.putfile(newDirname, fname)
			if err != nil {
				s.err("upload %s: %s", fname, err)
			}
		}
	}
	s.info("uploaded: %s", localDir)
	return nil
}
