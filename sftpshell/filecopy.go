package sftpshell

import (
	"errors"
	"fmt"
	"github.com/pkg/sftp"
	"github.com/scylladb/go-set/strset"
	"github.com/stephane-martin/vssh/sys"
	"io"
	"os"
	"path/filepath"
	"syscall"
)

func copyFileRemote(from, to string, client *sftp.Client) error {
	fromFile, err := client.Open(from)
	if err != nil {
		return fmt.Errorf("open failed for %s: %s", from, err)
	}
	defer func() { _ = fromFile.Close() }()
	toFile, err := client.Create(to)
	if err != nil {
		return fmt.Errorf("create failed for %s: %s", to, err)
	}
	_, err = io.Copy(toFile, fromFile)
	_ = toFile.Close()
	if err != nil {
		_ = client.Remove(to)
		return fmt.Errorf("copy from %s to %s failed: %s", from, to, err)
	}
	return nil
}

func copyFileLocal(from, to string) error {
	fromFile, err := os.Open(from)
	if err != nil {
		return err
	}
	defer func() { _ = fromFile.Close() }()
	toFile, err := os.Create(to)
	if err != nil {
		return err
	}
	_, err = io.Copy(toFile, fromFile)
	_ = toFile.Close()
	if err != nil {
		_ = os.Remove(to)
		return err
	}
	return nil
}

func (s *ShellState) copyDirLocal(from, to string) error {
	err := filepath.Walk(from, func(path string, info os.FileInfo, e error) error {
		if e != nil {
			return e
		}
		path = rel(from, path)
		if info.IsDir() {
			return os.Mkdir(join(to, path), 0700)
		} else if info.Mode().IsRegular() {
			return copyFileLocal(join(from, path), join(to, path))
		} else if isLink(info) {
			linkDest, err := os.Readlink(join(from, path))
			if err != nil {
				return err
			}
			return os.Symlink(linkDest, join(to, path))
		}
		return nil
	})
	if err != nil {
		_ = os.RemoveAll(to)
	}
	return err
}

func (s *ShellState) copyDirRemote(from, to string) (e error) {
	defer func() {
		if e != nil {
			_ = _rmdir(s.client, to)
		}
	}()
	walker := s.client.Walk(from)
	for walker.Step() {
		if walker.Err() != nil {
			return fmt.Errorf("walker error for %s: %s", walker.Path(), walker.Err())
		}
		path := walker.Path()
		info := walker.Stat()
		path = rel(from, path)
		if info.IsDir() {
			s.info("mkdir %s", join(to, path))
			err := s.client.Mkdir(join(to, path))
			if err != nil {
				return fmt.Errorf("mkdir failed for %s: %s", join(to, path), err)
			}
		} else if info.Mode().IsRegular() {
			s.info("copy file from %s to %s", join(from, path), join(to, path))
			err := copyFileRemote(join(from, path), join(to, path), s.client)
			if err != nil {
				return err
			}
		} else if isLink(info) {
			linkDest, err := s.client.ReadLink(join(from, path))
			if err != nil {
				return fmt.Errorf("readlink failed for %s: %s", join(from, path), err)
			}
			s.info("symlink from %s to %s", join(to, path), linkDest)
			err = s.client.Symlink(linkDest, join(to, path))
			if err != nil {
				return fmt.Errorf("syslink failed for %s: %s", linkDest, err)
			}
		}
	}
	return nil
}

func (s *ShellState) lcpdir(from, to string) error {
	_, err := os.Stat(to)
	if err == nil {
		return fmt.Errorf("destination %s already exists", to)
	}
	if !os.IsNotExist(err) {
		return err
	}

	err = s.copyDirLocal(from, to)
	if err == nil {
		// fix permissions
		_ = filepath.Walk(from, func(path string, info os.FileInfo, e error) error {
			if e != nil {
				return nil
			}
			path = rel(from, path)
			uid, gid := sys.UserGroupNum(info)
			if uid != -1 && gid != -1 {
				_ = os.Lchown(join(to, path), uid, gid)
			}
			if !isLink(info) {
				_ = os.Chmod(join(to, path), info.Mode().Perm())
			}
			return nil
		})
	}
	return err
}

func (s *ShellState) lmvdir(from, to string) error {
	err := os.Rename(from, to)
	if err == nil {
		return nil
	}
	if e, ok := err.(*os.LinkError); ok {
		if erno, ok := e.Err.(syscall.Errno); ok {
			if erno == 18 {
				// cross-device move directory
				err := s.lcpdir(from, to)
				if err == nil {
					return os.RemoveAll(from)
				}
				return err
			}
		}
	}
	return err
}

func (s *ShellState) cpdir(from, to string) error {
	s.info("copy directory from %s to %s", from, to)
	_, err := s.client.Stat(to)
	if err == nil {
		return fmt.Errorf("destination %s already exists", to)
	}
	if !os.IsNotExist(err) {
		return fmt.Errorf("stat error for %s: %s", to, err)
	}
	err = s.copyDirRemote(from, to)
	if err != nil {
		return err
	}
	// fix permissions
	s.info("fix permissions on destination %s", from)
	walker := s.client.Walk(from)
	for walker.Step() {
		if walker.Err() != nil {
			s.err("walker error for %s: %s", walker.Path(), walker.Err())
			continue
		}
		path := walker.Path()
		info := walker.Stat()
		path = rel(from, path)
		s.info("fix permissions on %s", join(to, path))
		uid, gid := sys.UserGroupNum(info)
		if !isLink(info) {
			if uid != -1 && gid != -1 {
				_ = s.client.Chown(join(to, path), uid, gid)
			}
			_ = s.client.Chmod(join(to, path), info.Mode().Perm())
		}
	}
	return nil
}

func (s *ShellState) mvdir(from, to string) error {
	err := s.client.Rename(from, to)
	if err == nil {
		s.info("renamed %s to %s", from, to)
		return nil
	}
	err = s.cpdir(from, to)
	if err != nil {
		return err
	}
	err = _rmdir(s.client, from)
	if err != nil {
		return fmt.Errorf("remove original directory %s failed: %s", from, err)
	}
	return nil
}

func (s *ShellState) cp(args []string, flags *strset.Set) error {
	// TODO: multiple sources
	if len(args) != 2 {
		return errors.New("cp takes two arguments")
	}
	from := join(s.RemoteWD, args[0])
	to := join(s.RemoteWD, args[1])
	statsF, err := s.client.Stat(from)
	if err != nil {
		return fmt.Errorf("stat failed for %s: %s", from, err)
	}
	statsT, err := s.client.Stat(to)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("stat failed for %s: %s", to, err)
	}
	if err == nil && statsT.IsDir() {
		to = join(to, filepath.Base(from))
	}
	if statsF.IsDir() {
		return s.cpdir(from, to)
	}
	info, err := s.client.Stat(to)
	if err == nil && info.IsDir() {
		return fmt.Errorf("destination exists and is a directory: %s", to)
	}
	if !os.IsNotExist(err) {
		return fmt.Errorf("stat failed for %s: %s", to, err)
	}
	err = copyFileRemote(from, to, s.client)
	if err != nil {
		return fmt.Errorf("file copy from %s to %s failed: %s", from, to, err)
	}
	uid, gid := sys.UserGroupNum(statsF)
	if !isLink(statsF) {
		if uid != -1 && gid != -1 {
			_ = s.client.Chown(to, uid, gid)
		}
		_ = s.client.Chmod(to, statsF.Mode().Perm())
	}
	//_ = os.Chtimes()
	return nil
}

func (s *ShellState) mv(args []string, flags *strset.Set) error {
	if len(args) != 2 {
		return errors.New("mv takes two arguments")
	}
	from := join(s.RemoteWD, args[0])
	to := join(s.RemoteWD, args[1])
	statsF, err := s.client.Stat(from)
	if err != nil {
		return err
	}
	statsT, err := s.client.Stat(to)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	if err == nil && statsT.IsDir() {
		to = join(to, filepath.Base(from))
	}
	if statsF.IsDir() {
		return s.mvdir(from, to)
	}
	err = s.client.Rename(from, to)
	if err == nil {
		return nil
	}
	info, err := s.client.Stat(to)
	if err == nil && info.IsDir() {
		return fmt.Errorf("destination exists and is a directory: %s", to)
	}
	if !os.IsNotExist(err) {
		return fmt.Errorf("stat failed for %s: %s", to, err)
	}
	// cross-device move file
	err = copyFileRemote(from, to, s.client)
	if err != nil {
		return fmt.Errorf("file copy from %s to %s failed: %s", from, to, err)
	}
	uid, gid := sys.UserGroupNum(statsF)
	if !isLink(statsF) {
		if uid != -1 && gid != -1 {
			_ = s.client.Chown(to, uid, gid)
		}
		_ = s.client.Chmod(to, statsF.Mode().Perm())
	}
	//_ = os.Chtimes()
	err = s.client.Remove(from)
	if err != nil {
		return fmt.Errorf("remove original file %s failed: %s", from, err)
	}
	return nil
}

func (s *ShellState) lcp(args []string, flags *strset.Set) error {
	if len(args) != 2 {
		return errors.New("lcp takes two arguments")
	}
	from := join(s.LocalWD, args[0])
	to := join(s.LocalWD, args[1])
	statsF, err := os.Stat(from)
	if err != nil {
		return err
	}
	statsT, err := os.Stat(to)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	if err == nil && statsT.IsDir() {
		to = join(to, filepath.Base(from))
	}
	if statsF.IsDir() {
		return s.lcpdir(from, to)
	}
	err = copyFileLocal(from, to)
	if err != nil {
		return err
	}
	uid, gid := sys.UserGroupNum(statsF)
	if uid != -1 && gid != -1 {
		_ = os.Lchown(to, uid, gid)
	}
	if !isLink(statsF) {
		_ = os.Chmod(to, statsF.Mode().Perm())
	}
	return nil
}

func (s *ShellState) lmv(args []string, flags *strset.Set) error {
	if len(args) != 2 {
		return errors.New("lmv takes two arguments")
	}
	from := join(s.LocalWD, args[0])
	to := join(s.LocalWD, args[1])
	statsF, err := os.Stat(from)
	if err != nil {
		return err
	}
	statsT, err := os.Stat(to)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	if err == nil && statsT.IsDir() {
		to = join(to, filepath.Base(from))
	}
	if statsF.IsDir() {
		return s.lmvdir(from, to)
	}
	err = os.Rename(from, to)
	if err == nil {
		return nil
	}
	if e, ok := err.(*os.LinkError); ok {
		if erno, ok := e.Err.(syscall.Errno); ok {
			if erno == 18 {
				// cross-device move file
				err := copyFileLocal(from, to)
				if err != nil {
					return err
				}
				uid, gid := sys.UserGroupNum(statsF)
				if uid != -1 && gid != -1 {
					_ = os.Lchown(to, uid, gid)
				}
				if !isLink(statsF) {
					_ = os.Chmod(to, statsF.Mode().Perm())
				}
				//_ = os.Chtimes()
				return os.Remove(from)
			}
		}
	}
	return err
}
