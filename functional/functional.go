package functional

import "strings"

func MapSlice(m []string, f func(string) string) {
	for i := range m {
		m[i] = f(m[i])
	}
}

func FilterSlice(m []string, f func(string) bool) []string {
	t := m[0:0]
	for i := range m {
		if f(m[i]) {
			t = append(t, m[i])
		}
	}
	return t
}

func JoinSlices(sep string, slices ...[]string) string {
	if len(slices) == 0 {
		return ""
	}
	var buf strings.Builder
	first := true
	for _, s := range slices {
		if len(s) > 0 {
			if !first {
				buf.WriteString(sep)
			}
			first = false
			buf.WriteString(strings.Join(s, sep))
		}
	}
	return buf.String()
}
