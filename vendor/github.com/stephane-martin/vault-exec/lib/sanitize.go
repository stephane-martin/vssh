package lib

import "strings"

var illegals = []string{"/", "=", "'", "-", " ", `"`}

func Sanitize(s string) string {
	for _, illegal := range illegals {
		s = strings.Replace(s, illegal, "_", -1)
	}
	return s
}
