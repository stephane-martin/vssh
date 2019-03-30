package lib

import (
	"bufio"
	"errors"
	"io"
	"unicode"
)

const (
	VIS_OCTAL   = 0x01
	VIS_CSTYLE  = 0x02
	VIS_SP      = 0x04 /* also encode space */
	VIS_TAB     = 0x08 /* also encode tab */
	VIS_NL      = 0x10 /* also encode newline */
	VIS_WHITE   = VIS_SP | VIS_TAB | VIS_NL
	VIS_SAFE    = 0x20  /* only encode "unsafe" characters */
	VIS_DQ      = 0x200 /* backslash-escape double quotes */
	VIS_ALL     = 0x400 /* encode all characters */
	VIS_NOSLASH = 0x40  /* inhibit printing '\' */
	VIS_GLOB    = 0x100 /* encode glob(3) magics and '#' */

	UNVIS_VALID     = 1  /* character valid */
	UNVIS_VALIDPUSH = 2  /* character valid, push back passed char */
	UNVIS_NOCHAR    = 3  /* valid sequence, no character produced */
	UNVIS_SYNBAD    = -1 /* unrecognized escape sequence */
	UNVIS_ERROR     = -2 /* decoder in unknown state (unrecoverable) */
	UNVIS_END       = 1  /* no more characters */

	S_GROUND = 0 /* haven't seen escape char */
	S_START  = 1 /* start decoding special sequence */
	S_META   = 2 /* metachar started (M) */
	S_META1  = 3 /* metachar more, regular char (-) */
	S_CTRL   = 4 /* control char started (^) */
	S_OCTAL2 = 5 /* octal digit 2 */
	S_OCTAL3 = 6 /* octal digit 3 */

)

var C = map[string]int{
	"OCTAL":   VIS_OCTAL,
	"CSTYLE":  VIS_CSTYLE,
	"SP":      VIS_SP,
	"TAB":     VIS_TAB,
	"NL":      VIS_NL,
	"SAFE":    VIS_SAFE,
	"DQ":      VIS_DQ,
	"ALL":     VIS_ALL,
	"NOSLASH": VIS_NOSLASH,
	"GLOB":    VIS_GLOB,
	"WHITE":   VIS_WHITE,
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

func BytesVis(dst, src []byte, flag int) []byte {
	if len(src) == 0 {
		return dst[0:0]
	}
	if cap(dst) < 4*len(src) {
		dst = make([]byte, 0, 4*len(src))
	}
	dst = dst[0:0]
	for i := 0; i < len(src)-1; i++ {
		dst = Vis(dst, src[i], flag, src[i+1])
	}
	return Vis(dst, src[len(src)-1], flag, 0)
}

func StrVis(src string, flag int) string {
	if len(src) == 0 {
		return ""
	}
	return string(BytesVis(nil, []byte(src), flag))
}

func StreamVis(r io.Reader, w io.Writer, flag int) error {
	reader := bufio.NewReader(r)
	buf := make([]byte, 0, 4)
	var next byte
	for {
		b, err := reader.ReadByte()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		n, err := reader.Peek(1)
		if err != nil {
			next = 0
		} else {
			next = n[0]
		}
		buf = Vis(buf[0:0], b, flag, next)
		_, err = w.Write(buf)
		if err != nil {
			return err
		}
	}
}

func unvis(dst *byte, c byte, astate *int, flag int) int {
	if flag&UNVIS_END != 0 {
		if *astate == S_OCTAL2 || *astate == S_OCTAL3 {
			*astate = S_GROUND
			return UNVIS_VALID
		}
		if *astate == S_GROUND {
			return UNVIS_NOCHAR
		}
		return UNVIS_SYNBAD
	}

	switch *astate {

	case S_GROUND:
		*dst = 0
		if c == '\\' {
			*astate = S_START
			return 0
		}
		*dst = c
		return UNVIS_VALID

	case S_START:
		switch c {
		case '-':
			*dst = 0
			*astate = S_GROUND
			return 0
		case '\\', '"':
			*dst = c
			*astate = S_GROUND
			return UNVIS_VALID
		case '0', '1', '2', '3', '4', '5', '6', '7':
			*dst = c - '0'
			*astate = S_OCTAL2
			return 0
		case 'M':
			*dst = 0200
			*astate = S_META
			return 0
		case '^':
			*astate = S_CTRL
			return 0
		case 'n':
			*dst = '\n'
			*astate = S_GROUND
			return UNVIS_VALID
		case 'r':
			*dst = '\r'
			*astate = S_GROUND
			return UNVIS_VALID
		case 'b':
			*dst = '\b'
			*astate = S_GROUND
			return UNVIS_VALID
		case 'a':
			*dst = '\007'
			*astate = S_GROUND
			return UNVIS_VALID
		case 'v':
			*dst = '\v'
			*astate = S_GROUND
			return UNVIS_VALID
		case 't':
			*dst = '\t'
			*astate = S_GROUND
			return UNVIS_VALID
		case 'f':
			*dst = '\f'
			*astate = S_GROUND
			return UNVIS_VALID
		case 's':
			*dst = ' '
			*astate = S_GROUND
			return UNVIS_VALID
		case 'E':
			*dst = '\033'
			*astate = S_GROUND
			return UNVIS_VALID
		case '\n':
			*astate = S_GROUND
			return UNVIS_NOCHAR
		case '$':
			*astate = S_GROUND
			return UNVIS_NOCHAR
		}
		*astate = S_GROUND
		return UNVIS_SYNBAD

	case S_META:
		if c == '-' {
			*astate = S_META1
		} else if c == '^' {
			*astate = S_CTRL
		} else {
			*astate = S_GROUND
			return UNVIS_SYNBAD
		}
		return 0

	case S_META1:
		*astate = S_GROUND
		*dst |= c
		return UNVIS_VALID

	case S_CTRL:
		if c == '?' {
			*dst |= 0177
		} else {
			*dst |= c & 037
		}
		*astate = S_GROUND
		return UNVIS_VALID

	case S_OCTAL2: /* second possible octal digit */
		if IsOctal(c) {
			*dst = (*dst << 3) + (c - '0')
			*astate = S_OCTAL3
			return 0
		}
		*astate = S_GROUND
		return UNVIS_VALIDPUSH

	case S_OCTAL3: /* third possible octal digit */
		*astate = S_GROUND
		if IsOctal(c) {
			*dst = (*dst << 3) + (c - '0')
			return UNVIS_VALID
		}
		return UNVIS_VALIDPUSH

	default:
		*astate = S_GROUND
		return UNVIS_SYNBAD
	}
}

func BytesUnvis(dst, src []byte) ([]byte, error) {
	if len(src) == 0 {
		return dst[0:0], nil
	}
	var i, state int
	var c byte
	dst = append(dst[0:0], 0)

	for _, c = range src {
	again:
		switch unvis(&dst[i], c, &state, 0) {
		case UNVIS_VALID:
			dst = append(dst, 0)
			i++
		case UNVIS_VALIDPUSH:
			dst = append(dst, 0)
			i++
			goto again
		case 0, UNVIS_NOCHAR:
		default:
			return dst[0:0], errors.New("can't decode")
		}
	}
	if unvis(&dst[i], c, &state, UNVIS_END) == UNVIS_VALID {
		return dst, nil
	}
	return dst[0:len(dst)-1], nil
}

func StrUnvis(src string) (string, error) {
	if len(src) == 0 {
		return "", nil
	}
	dst, err := BytesUnvis(make([]byte, 0, len(src)), []byte(src))
	if err != nil {
		return "", err
	}
	return string(dst), nil
}
