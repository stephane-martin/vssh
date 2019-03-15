package main

import (
	"fmt"
	"os"
	"os/exec"

	"go.uber.org/zap"
)

func Native(privkeyPath, ruser, rhost string, port int, args []string, verbose, insecure, force bool, l *zap.SugaredLogger) error {
	var allArgs []string
	if verbose {
		allArgs = append(allArgs, "-v")
	}
	if insecure {
		allArgs = append(allArgs, "-o", "StrictHostKeyChecking=no", "-o", "UserKnownHostsFile=/dev/null")
	}
	if force {
		allArgs = append(allArgs, "-t")
	}
	allArgs = append(allArgs, "-i", privkeyPath, "-l", ruser, "-p", fmt.Sprintf("%d", port), rhost)
	allArgs = append(allArgs, args...)
	cmd := exec.Command("ssh", allArgs...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}
