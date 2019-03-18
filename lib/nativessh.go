package lib

import (
	"fmt"
	"os"
	"os/exec"

	"go.uber.org/zap"
)

func Native(sshParams SSHParams, privkeyPath string, l *zap.SugaredLogger) error {
	var allArgs []string
	if sshParams.Verbose {
		allArgs = append(allArgs, "-v")
	}
	if sshParams.Insecure {
		allArgs = append(allArgs, "-o", "StrictHostKeyChecking=no", "-o", "UserKnownHostsFile=/dev/null")
	}
	if sshParams.ForceTerminal {
		allArgs = append(allArgs, "-t")
	}
	allArgs = append(allArgs, "-i", privkeyPath, "-l", sshParams.LoginName, "-p", fmt.Sprintf("%d", sshParams.Port), sshParams.Host)
	allArgs = append(allArgs, sshParams.Commands...)
	cmd := exec.Command("ssh", allArgs...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}
