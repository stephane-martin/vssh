package lib

import "strings"

func sanitize(s string) string {
	return strings.Replace(s, "/", "_", -1)
}
