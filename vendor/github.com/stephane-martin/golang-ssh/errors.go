package ssh

import (
	"strconv"
	"strings"

	"golang.org/x/crypto/ssh"
)

// ExitError is a conveniance wrapper for (crypto/ssh).ExitError type.
type ExitError struct {
	Err      error
	ExitCode int
}

// Error implements error interface.
func (err *ExitError) Error() string {
	return err.Err.Error()
}

// Cause implements errors.Causer interface.
func (err *ExitError) Cause() error {
	return err.Err
}

func wrapError(err error) error {
	if err == nil {
		return nil
	}
	switch err := err.(type) {
	case *ssh.ExitError:
		e, s := &ExitError{Err: err, ExitCode: -1}, strings.TrimSpace(err.Error())
		// Best-effort attempt to parse exit code from os/exec error string,
		// like "Process exited with status 127".
		if i := strings.LastIndex(s, " "); i != -1 {
			if n, err := strconv.Atoi(s[i+1:]); err == nil {
				e.ExitCode = n
			}
		}
		return e
	default:
		return err
	}
}

func nonil(err ...error) error {
	for _, e := range err {
		if e != nil {
			return e
		}
	}
	return nil
}
