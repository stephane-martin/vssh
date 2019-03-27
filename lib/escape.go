package lib

import (
	"fmt"
	"strings"
)

// EscapeEnv escapes a map of environment variables to be used by the env command.
func EscapeEnv(env map[string]string) []string {
	if len(env) == 0 {
		return nil
	}
	r := make([]string, 0, len(env))
	for k, v := range env {
		r = append(r, fmt.Sprintf(`%s=%s`, k, EscapeString(v)))
	}
	return r
}

// EscapeString escapes a string for shell usage.
func EscapeString(s string) string {
	return "'" + strings.Replace(s, "'", `'"'"'`, -1) + "'"
}
