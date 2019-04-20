package mimeapps

import (
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/gdamore/tcell"
)

type ErrNoOpener struct {
	Filename string
	Err      error
}

func (e ErrNoOpener) Error() string {
	if e.Err == nil {
		return fmt.Sprintf("no opener found for: %s", e.Filename)
	}
	return fmt.Sprintf("no opener found for: %s (%s)", e.Filename, e.Err)
}

type execError struct {
	err error
}

func (e execError) Error() string { return e.err.Error() }

func isPlaceHolder(s string) bool {
	return s == `%f` || s == `%F` || s == `%u` || s == `%U`
}

func OpenLocal(filename string) error {
	stats, err := os.Stat(filename)
	if err != nil {
		return err
	}
	if stats.IsDir() {
		return fmt.Errorf("is a directory: %s", filename)
	}
	if !stats.Mode().IsRegular() {
		return fmt.Errorf("not a regular file: %s", filename)
	}
	opener, terminal, err := FilenameToApplication(filename)
	if err != nil {
		return ErrNoOpener{Filename: filename, Err: err}
	}
	var found bool
	for i := range opener {
		if isPlaceHolder(opener[i]) {
			opener[i] = filename
			found = true
		}
	}
	if !found {
		opener = append(opener, filename)
	}
	cmd := exec.Command(opener[0], opener[1:]...)
	if terminal {
		var scr tcell.Screen
		scr, err = tcell.NewScreen()
		if err != nil {
			return err
		}
		err = scr.Init()
		if err != nil {
			return err
		}
		cmd.Stdin = os.Stdin
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		err = cmd.Run()
		scr.Fini()
	} else {
		err = cmd.Run()
	}
	if err == nil {
		return nil
	}
	return execError{err: err}
}

func OpenRemote(filename string, r io.Reader) (string, error) {
	tempDir, err := ioutil.TempDir("", "mimeapps-opener")
	if err != nil {
		return "", err
	}
	baseName := filepath.Base(filename)
	destName := filepath.Join(tempDir, baseName)
	dest, err := os.Create(destName)
	if err != nil {
		_ = os.RemoveAll(tempDir)
		return "", err
	}
	_, err = io.Copy(dest, r)
	if err != nil {
		_ = dest.Close()
		_ = os.RemoveAll(tempDir)
		return "", err
	}
	_ = dest.Close()
	_ = os.Chmod(destName, 0500)
	err = OpenLocal(destName)
	if err == nil {
		return destName, nil
	}
	if e, ok := err.(execError); ok {
		return destName, e.err
	}
	_ = os.RemoveAll(tempDir)
	if e, ok := err.(ErrNoOpener); ok {
		return "", ErrNoOpener{Filename: filename, Err: e.Err}
	}
	return "", err
}
