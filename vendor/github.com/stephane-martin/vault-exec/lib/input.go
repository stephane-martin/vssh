package lib

import (
	"bufio"
	"fmt"
	"os"
	"strings"
	"syscall"

	"golang.org/x/crypto/ssh/terminal"
)

func Input(text string, password bool) (string, error) {
	if password {
		fmt.Print(text)
		input, err := terminal.ReadPassword(syscall.Stdin)
		fmt.Println()
		if err != nil {
			return "", err
		}
		return strings.TrimSpace(string(input)), nil
	}
	reader := bufio.NewReader(os.Stdin)
	fmt.Print(text)
	input, err := reader.ReadString('\n')
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(input), nil
}
