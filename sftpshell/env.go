package sftpshell

import (
	"errors"
	"fmt"
	"github.com/scylladb/go-set/strset"
	"strconv"
	"strings"
)

func (s *ShellState) env(args []string, flags *strset.Set) error {
	if len(args) == 0 {
		for k, v := range s.environ {
			if v == "" {
				fmt.Fprintf(s.out, "%s=\n", k)
			} else {
				v = strconv.Quote(v)
				fmt.Fprintf(s.out, "%s=%s\n", k, v[1:len(v)-1])
			}
		}
		return nil
	}
	if len(args) == 1 {
		if v, ok := s.environ[args[0]]; ok {
			if v == "" {
				fmt.Fprintf(s.out, "%s=\n", args[0])
			} else {
				v = strconv.Quote(v)
				fmt.Fprintf(s.out, "%s=%s\n", args[0], v[1:len(v)-1])
			}
			return nil
		}
		return fmt.Errorf("no such environment variable: %s", args[0])
	}
	return errors.New("env takes zero or one argument")
}

func (s *ShellState) set(args []string, flags *strset.Set) error {
	if len(args) != 2 {
		return errors.New("set takes exactly 2 arguments")
	}
	if strings.Contains(args[0], "=") {
		return errors.New("environment variable key can not contain '='")
	}
	s.environ[args[0]] = args[1]
	return nil
}

func (s *ShellState) unset(args []string, flags *strset.Set) error {
	if len(args) != 1 {
		return errors.New("unset takes exactly one argument")
	}
	if _, ok := s.environ[args[0]]; !ok {
		return fmt.Errorf("no such environment variable: %s", args[0])
	}
	delete(s.environ, args[0])
	return nil
}
