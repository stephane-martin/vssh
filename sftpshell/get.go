package sftpshell

import (
	"github.com/scylladb/go-set/strset"
	"github.com/stephane-martin/vssh/remoteops"
	"io"
	"os"
)

func (s *ShellState) get(args []string, flags *strset.Set) error {
	remoteWD := s.RemoteWD
	if len(args) == 0 {
		names, err := remoteops.FuzzyRemote(s.client, remoteWD, nil)
		if err != nil {
			return err
		}
		if len(names) == 0 {
			return nil
		}
		args = names
	}
	var files, dirs []string
	for _, name := range args {
		path := join(remoteWD, name)
		stats, err := s.client.Stat(path)
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

	localWD := s.LocalWD
	for _, name := range dirs {
		err := s.getdir(localWD, name)
		if err != nil {
			s.err("download %s: %s", name, err)
		}
	}
	for _, name := range files {
		err := s.getfile(localWD, name)
		if err != nil {
			s.err("download %s: %s", name, err)
		}
	}
	return nil
}

func (s *ShellState) getfile(targetLocalDir, remoteFile string) error {
	source, err := s.client.Open(remoteFile)
	if err != nil {
		return err
	}
	defer func() { _ = source.Close() }()
	stats, err := source.Stat()
	if err != nil {
		return err
	}

	localFilename := join(targetLocalDir, base(remoteFile))
	dest, err := os.Create(localFilename)
	if err != nil {
		return err
	}
	defer func() { _ = dest.Close() }()
	s.info("download: %s", remoteFile)
	bar := newBar(stats.Size())
	_, err = io.Copy(dest, bar.NewProxyReader(source))
	bar.Finish()
	if err != nil {
		return err
	}
	s.info("downloaded: %s", remoteFile)
	return nil
}

func (s *ShellState) getdir(targetLocalDir, remoteDir string) error {
	files, err := s.client.ReadDir(remoteDir)
	if err != nil {
		return err
	}
	newDirname := join(targetLocalDir, base(remoteDir))
	err = os.Mkdir(newDirname, 0755)
	if err != nil && !os.IsExist(err) {
		return err
	}
	for _, f := range files {
		fname := join(remoteDir, f.Name())
		if f.IsDir() {
			err := s.getdir(newDirname, fname)
			if err != nil {
				s.err("download %s: %s", fname, err)
			}
		} else if f.Mode().IsRegular() {
			err := s.getfile(newDirname, fname)
			if err != nil {
				s.err("download %s: %s", fname, err)
			}
		}
	}
	s.info("downloaded: %s", remoteDir)
	return nil
}
