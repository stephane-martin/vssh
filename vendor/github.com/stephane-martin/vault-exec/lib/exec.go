package lib

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"

	"go.uber.org/zap"
)

var ErrForceStop = errors.New("command has been forced to stop")
var ErrCmdFinishedNoError = errors.New("command has finished without error")

func ExecCmd(ctx context.Context, args []string, results map[string]string, genv []string, l *zap.SugaredLogger) error {
	e := make([]string, 0, len(genv))
	e = append(e, genv...)
	cmd := exec.Command(args[0], args[1:]...)
	for k, v := range results {
		e = append(e, fmt.Sprintf("%s=%s", k, v))
	}
	cmd.Env = e
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	//cmd.SysProcAttr = &syscall.SysProcAttr{
	//	Setpgid: true,
	//	Pgid:    0,
	//}
	l.Infow("starting command", "command", strings.Join(args, " "))
	err := cmd.Start()
	if err != nil {
		return fmt.Errorf("failed to start command: %s", err)
	}
	forceStop := false
	go func() {
		<-ctx.Done()
		forceStop = true
		_ = cmd.Process.Signal(syscall.SIGTERM)
	}()
	err = cmd.Wait()
	if err != nil {
		if e, ok := err.(*exec.ExitError); ok {
			if e2, ok := e.Sys().(syscall.WaitStatus); ok {
				l.Infow("command exit status is non zero", "status", e2.ExitStatus(), "error", err)
			} else {
				l.Infow("command returned error", "error", err)
			}
		} else {
			l.Infow("command returned error", "error", err)
		}
		if forceStop {
			return ErrForceStop
		}
		return err
	}
	l.Infow("command returned")
	return ErrCmdFinishedNoError
}
