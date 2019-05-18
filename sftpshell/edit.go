package sftpshell

import (
	"bytes"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"

	"github.com/scylladb/go-set/strset"
	"github.com/stephane-martin/vssh/remoteops"
	"golang.org/x/crypto/ssh/terminal"
)

func (s *ShellState) edit(args []string, flags *strset.Set) error {
	editor := os.Getenv("EDITOR")
	if editor == "" {
		editor = "vim"
	}
	editorExe, err := exec.LookPath(editor)
	if err != nil {
		return err
	}
	allmatches := strset.New()
	if len(args) == 0 {
		files, err := remoteops.FuzzyRemote(s.client, s.RemoteWD, nil)
		if err != nil {
			return err
		}
		if len(files) == 0 {
			return nil
		}
		allmatches.Add(files...)
	} else {
		allmatches, err = findMatches(args, s.RemoteWD, s.client, onlyFiles)
		if err != nil {
			return err
		}
		nonExisting, err := nonExistingFiles(args, s.RemoteWD, s.client)
		if err != nil {
			return err
		}
		var created []string
		for _, fname := range nonExisting.List() {
			f, err := s.client.Create(fname)
			if err != nil {
				for _, fname2 := range created {
					_ = s.client.Remove(fname2)
				}
				return err
			}
			_ = f.Close()
			created = append(created, fname)
		}
		allmatches.Merge(nonExisting)
	}
	if allmatches.Size() == 0 {
		return nil
	}

	// remote file to edit => local filename
	tempFiles := make(map[string]string)
	initialHashes := make(map[string][]byte)

	copyTemp := func(match string) error {
		f, err := s.client.Open(match)
		if err != nil {
			return fmt.Errorf("failed to open remote file: %s", err)
		}
		defer func() { _ = f.Close() }()
		stats, err := f.Stat()
		if err != nil {
			return fmt.Errorf("failed to stat remote file: %s", err)
		}
		if stats.IsDir() || !stats.Mode().IsRegular() {
			return nil
		}
		// create a temp directory for each file to edit
		t, err := ioutil.TempDir("", "vssh-shell-edit")
		if err != nil {
			return fmt.Errorf("failed to make temp directory %s: %s", t, err)
		}
		dest := join(t, filepath.Base(f.Name()))
		destFile, err := os.Create(dest)
		if err != nil {
			return fmt.Errorf("failed to create local file %s: %s", dest, err)
		}
		defer func() { _ = destFile.Close() }()
		_, err = io.Copy(destFile, f)
		_ = destFile.Close()
		if err != nil {
			_ = os.RemoveAll(t)
			return fmt.Errorf("failed to copy remote file %s: %s", f.Name(), err)
		}
		err = os.Chmod(dest, stats.Mode().Perm()&0700)
		if err != nil {
			s.err("failed to chmod local copy %s: %s", dest, err)
		}
		h, err := hashLocalFile(dest)
		if err != nil {
			_ = os.RemoveAll(t)
			return fmt.Errorf("failed to hash local copy %s: %s", dest, err)
		}
		tempFiles[match] = dest
		initialHashes[match] = h
		return nil
	}

	for _, remoteFilename := range allmatches.List() {
		// copy remote files to temp directories
		err := copyTemp(remoteFilename)
		if err != nil {
			s.err("%s: %s", remoteFilename, err)
		}
	}
	if len(tempFiles) == 0 {
		return nil
	}
	tempFilesList := make([]string, 0, len(tempFiles))
	for _, tempFilename := range tempFiles {
		fname := tempFilename
		tempFilesList = append(tempFilesList, fname)
		defer func() {
			_ = os.RemoveAll(filepath.Dir(fname))
		}()
	}

	cmd := exec.Command(editorExe, tempFilesList...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	err = cmd.Run()
	if err != nil {
		return err
	}

	upload := func(remoteFilename, tempFilename string) error {
		local, err := os.Open(tempFilename)
		if err != nil {
			return err
		}
		defer func() { _ = local.Close() }()
		remote, err := s.client.Create(remoteFilename)
		if err != nil {
			return err
		}
		defer func() { _ = remote.Close() }()
		_, err = io.Copy(remote, local)
		return err
	}

	// copy back the modified files to the remote side if needed
	for remoteFilename, tempFilename := range tempFiles {
		previousHash := initialHashes[remoteFilename]
		newHash, err := hashLocalFile(tempFilename)
		if err != nil {
			return err
		}
		if !bytes.Equal(previousHash, newHash) {
			err := upload(remoteFilename, tempFilename)
			if err != nil {
				s.err("%s: %s", remoteFilename, err)
			} else {
				s.info("modified: %s", remoteFilename)
			}
		}

	}
	return nil
}

func (s *ShellState) ledit(args []string, flags *strset.Set) error {
	state, err := terminal.GetState(syscall.Stdin)
	if err != nil {
		return err
	}
	defer func() { _ = terminal.Restore(syscall.Stdin, state) }()
	editor := os.Getenv("EDITOR")
	if editor == "" {
		editor = "vim"
	}
	editorExe, err := exec.LookPath(editor)
	if err != nil {
		return err
	}
	allmatches := strset.New()
	if len(args) == 0 {
		files, err := remoteops.FuzzyLocal(s.LocalWD, nil)
		if err != nil {
			return err
		}
		if len(files) == 0 {
			return nil
		}
		allmatches.Add(files...)
	} else {
		var err error
		allmatches, err = findMatches(args, s.LocalWD, nil, onlyFiles)
		if err != nil {
			return err
		}
		nonExisting, err := nonExistingFiles(args, s.LocalWD, nil)
		if err != nil {
			return err
		}
		var created []string
		for _, fname := range nonExisting.List() {
			f, err := os.Create(fname)
			if err != nil {
				for _, fname2 := range created {
					_ = os.Remove(fname2)
				}
				return err
			}
			_ = f.Close()
			created = append(created, fname)
		}
		allmatches.Merge(nonExisting)
	}
	if allmatches.Size() == 0 {
		return nil
	}
	cmd := exec.Command(editorExe, allmatches.List()...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}
