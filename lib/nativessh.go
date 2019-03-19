package lib

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"go.uber.org/zap"
)

func Native(sshParams SSHParams, privkeyPath, certPath string, env map[string]string, l *zap.SugaredLogger) error {
	var allArgs []string
	if sshParams.Verbose {
		allArgs = append(allArgs, "-v")
	}
	if sshParams.ForceTerminal {
		allArgs = append(allArgs, "-t")
	}

	if sshParams.Insecure {
		allArgs = append(allArgs, "-o", "StrictHostKeyChecking=no", "-o", "UserKnownHostsFile=/dev/null")
	}
	allArgs = append(
		allArgs,
		"-o", fmt.Sprintf("IdentityFile=%s", privkeyPath),
		"-o", fmt.Sprintf("CertificateFile=%s", certPath),
		"-l", strings.Replace(sshParams.LoginName, " ", `\ `, -1),
		"-p", fmt.Sprintf("%d", sshParams.Port),
		sshParams.Host,
	)
	if len(env) != 0 {
		var pre []string
		pre = append(pre, "env")
		pre = append(pre, EscapeEnv(env)...)
		if len(sshParams.Commands) == 0 {
			pre = append([]string{"-t"}, pre...)
			pre = append(pre, "bash")
		}
		allArgs = append(allArgs, pre...)
	}
	allArgs = append(allArgs, sshParams.Commands...)
	cmd := exec.Command("ssh", allArgs...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}
