package lib

import (
	"os"
	"strings"
	"unicode"
)

func ForwardEnv(forward string) []string {
	env := make([]string, 0)
	if forward == "*" {
		return os.Environ()
	}
	if forward == "" {
		return env
	}
	m := make(map[string]bool)
	for _, k := range strings.FieldsFunc(forward, func(r rune) bool {
		return r == ',' || unicode.IsSpace(r)
	}) {
		m[k] = true
	}
	for _, v := range os.Environ() {
		v = strings.TrimLeft(v, "= ")
		if v != "" {
			spl := strings.SplitN(v, "=", 2)
			if m[spl[0]] {
				env = append(env, v)
			}
		}
	}
	return env
}
