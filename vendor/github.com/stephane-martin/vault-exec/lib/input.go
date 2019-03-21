package lib

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"syscall"

	"golang.org/x/crypto/ssh/terminal"
)

func Input(text string, password bool) ([]byte, error) {
	if password {
		fmt.Print(text)
		input, err := terminal.ReadPassword(syscall.Stdin)
		fmt.Println()
		if err != nil {
			return nil, err
		}
		return bytes.TrimSpace(input), nil
	}
	reader := bufio.NewReader(os.Stdin)
	fmt.Print(text)
	input, err := reader.ReadBytes('\n')
	if err != nil {
		return nil, err
	}
	return bytes.TrimSpace(input), nil
}
