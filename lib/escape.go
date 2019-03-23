package lib

import (
	"fmt"
	"strings"
)

func EscapeEnv(env map[string]string) []string {
	if len(env) == 0 {
		return nil
	}
	var r []string
	for k, v := range env {
		r = append(r, fmt.Sprintf(`%s=%s`, k, EscapeString(v)))
	}
	return r
}

func EscapeString(s string) string {
	return "'" + strings.Replace(s, "'", `'"'"'`, -1) + "'"
}
