package lib

import (
	"errors"
	"strings"
	"unicode"
)

const (
	VIS_OCTAL       = 0x01
	VIS_CSTYLE      = 0x02
	VIS_SP          = 0x04 /* also encode space */
	VIS_TAB         = 0x08 /* also encode tab */
	VIS_NL          = 0x10 /* also encode newline */
	VIS_WHITE       = VIS_SP | VIS_TAB | VIS_NL
	VIS_SAFE        = 0x20  /* only encode "unsafe" characters */
	VIS_DQ          = 0x200 /* backslash-escape double quotes */
	VIS_ALL         = 0x400 /* encode all characters */
	VIS_NOSLASH     = 0x40  /* inhibit printing '\' */
	VIS_GLOB        = 0x100 /* encode glob(3) magics and '#' */

	UNVIS_VALID     = 1     /* character valid */
	UNVIS_VALIDPUSH = 2     /* character valid, push back passed char */
	UNVIS_NOCHAR    = 3     /* valid sequence, no character produced */
	UNVIS_SYNBAD    = -1    /* unrecognized escape sequence */
	UNVIS_ERROR     = -2    /* decoder in unknown state (unrecoverable) */
	UNVIS_END       = 1     /* no more characters */
)

var C = map[string]int{
	"OCTAL": VIS_OCTAL,
	"CSTYLE": VIS_CSTYLE,
	"SP": VIS_SP,
	"TAB": VIS_TAB,
	"NL": VIS_NL,
	"SAFE": VIS_SAFE,
	"DQ": VIS_DQ,
	"ALL": VIS_ALL,
	"NOSLASH": VIS_NOSLASH,
	"GLOB": VIS_GLOB,
	"WHITE": VIS_WHITE,
}

func IsOctal(c byte) bool {
	return c >= '0' && c <= '7'
}

func IsGraph(c byte) bool {
	if c == ' ' {
		return false
	}
	return unicode.IsGraphic(rune(c))
}

func IsVisible(c byte, flag int) bool {
	isgraph := IsGraph(c)
	var c1 = c == '\\' || (flag&VIS_ALL) == 0
	var c3 = c != '*' && c != '?' && c != '[' && c != '#'
	var c4 = c3 || (flag&VIS_GLOB) == 0
	var c5 = c <= 127 && c4 && isgraph
	var c6 = (flag&VIS_SP) == 0 && c == ' '
	var c7 = (flag&VIS_TAB) == 0 && c == '\t'
	var c8 = (flag&VIS_NL) == 0 && c == '\n'
	var c9 = c == '\b' || c == '\007' || c == '\r' || isgraph
	var c10 = (flag&VIS_SAFE) != 0 && c9
	return c1 && (c5 || c6 || c7 || c8 || c10)
}

func Vis(dst []byte, c byte, flag int, nextc byte) []byte {

	if IsVisible(c, flag) {
		if (c == '"' && (flag&VIS_DQ) != 0) || (c == '\\' && (flag&VIS_NOSLASH) == 0) {
			dst = append(dst, '\\')
		}
		dst = append(dst, c)
		return dst
	}

	if (flag & VIS_CSTYLE) != 0 {
		switch c {
		case '\n':
			dst = append(dst, '\\', 'n')
			return dst
		case '\r':
			dst = append(dst, '\\', 'r')
			return dst
		case '\b':
			dst = append(dst, '\\', 'b')
			return dst
		case '\a':
			dst = append(dst, '\\', 'a')
			return dst
		case '\v':
			dst = append(dst, '\\', 'v')
			return dst
		case '\t':
			dst = append(dst, '\\', 't')
			return dst
		case '\f':
			dst = append(dst, '\\', 'f')
			return dst
		case ' ':
			dst = append(dst, '\\', 's')
			return dst
		case 0:
			dst = append(dst, '\\', '0')
			if IsOctal(nextc) {
				dst = append(dst, '0', '0')
			}
			return dst
		}
	}

	if ((c & 0177) == ' ') || (flag&VIS_OCTAL) != 0 || ((flag&VIS_GLOB) != 0 && (c == '*' || c == '?' || c == '[' || c == '#')) {
		dst = append(
			dst,
			'\\',
			(c>>6&07)+'0',
			(c>>3&07)+'0',
			(c&07)+'0',
		)
		return dst
	}
	if (flag & VIS_NOSLASH) == 0 {
		dst = append(dst, '\\')
	}
	if (c & 0200) != 0 {
		c &= 0177
		dst = append(dst, 'M')
	}
	if unicode.IsControl(rune(c)) {
		dst = append(dst, '^')
		if c == 0177 {
			dst = append(dst, '?')
		} else {
			dst = append(dst, c+'@')
		}
	} else {
		dst = append(dst, '-', c)
	}
	return dst
}

func StrVis(s string, flag int) string {
	if len(s) == 0 {
		return ""
	}
	buf := make([]byte, 0, 4*len(s))
	for i := 0; i < len(s)-1; i++ {
		buf = Vis(buf, s[i], flag, s[i+1])
	}
	buf = Vis(buf, s[len(s)-1], flag, 0)
	return string(buf)
}

func StrVisE(s string, flag string) (string, error) {
	if f, ok := C[strings.ToUpper(flag)]; ok {
		return StrVis(s, f), nil
	}
	return "", errors.New("unknown flag")
}