package main

import "strings"

func mapSlice(m []string, f func(string) string) {
	for i := range m {
		m[i] = f(m[i])
	}
}

func filterSlice(m []string, f func(string) bool) []string {
	t := m[0:0]
	for i := range m {
		if f(m[i]) {
			t = append(t, m[i])
		}
	}
	return t
}

func joinSlices(slices ...[]string) string {
	var buf strings.Builder
	first := true
	for _, s := range slices {
		if len(s) > 0 {
			if !first {
				buf.WriteByte(' ')
			}
			first = false
			buf.WriteString(strings.Join(s, " "))
		}
	}
	return buf.String()
}
