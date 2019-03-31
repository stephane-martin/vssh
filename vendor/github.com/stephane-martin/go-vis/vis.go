package vis

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

func StreamVis(w io.Writer, r io.Reader, flag int) error {
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

type unvis struct {
	dst    byte
	astate int
}

func (s *unvis) end() (byte, int) {
	if s.astate == S_OCTAL2 || s.astate == S_OCTAL3 {
		s.astate = S_GROUND
		return s.dst, UNVIS_VALID
	}
	if s.astate == S_GROUND {
		return 0, UNVIS_NOCHAR
	}
	return 0, UNVIS_SYNBAD

}

func (s *unvis) unvis(c byte) (byte, int) {
	switch s.astate {

	case S_GROUND:
		s.dst = 0
		if c == '\\' {
			s.astate = S_START
			return 0, 0
		}
		return c, UNVIS_VALID

	case S_START:
		switch c {
		case '-':
			s.dst = 0
			s.astate = S_GROUND
			return 0, 0
		case '\\', '"':
			s.dst = 0
			s.astate = S_GROUND
			return c, UNVIS_VALID
		case '0', '1', '2', '3', '4', '5', '6', '7':
			s.dst = c - '0'
			s.astate = S_OCTAL2
			return 0, 0
		case 'M':
			s.dst = 0200
			s.astate = S_META
			return 0, 0
		case '^':
			s.astate = S_CTRL
			return 0, 0
		case 'n':
			s.dst = 0
			s.astate = S_GROUND
			return '\n', UNVIS_VALID
		case 'r':
			s.dst = 0
			s.astate = S_GROUND
			return '\r', UNVIS_VALID
		case 'b':
			s.dst = 0
			s.astate = S_GROUND
			return '\b', UNVIS_VALID
		case 'a':
			s.dst = 0
			s.astate = S_GROUND
			return '\007', UNVIS_VALID
		case 'v':
			s.dst = 0
			s.astate = S_GROUND
			return '\v', UNVIS_VALID
		case 't':
			s.dst = 0
			s.astate = S_GROUND
			return '\t', UNVIS_VALID
		case 'f':
			s.dst = 0
			s.astate = S_GROUND
			return '\f', UNVIS_VALID
		case 's':
			s.dst = 0
			s.astate = S_GROUND
			return ' ', UNVIS_VALID
		case 'E':
			s.dst = 0
			s.astate = S_GROUND
			return '\033', UNVIS_VALID
		case '\n':
			s.astate = S_GROUND
			return 0, UNVIS_NOCHAR
		case '$':
			s.astate = S_GROUND
			return 0, UNVIS_NOCHAR
		}
		s.astate = S_GROUND
		return 0, UNVIS_SYNBAD

	case S_META:
		if c == '-' {
			s.astate = S_META1
		} else if c == '^' {
			s.astate = S_CTRL
		} else {
			s.astate = S_GROUND
			return 0, UNVIS_SYNBAD
		}
		return 0, 0

	case S_META1:
		s.astate = S_GROUND
		res := s.dst | c
		s.dst = 0
		return res, UNVIS_VALID

	case S_CTRL:
		s.astate = S_GROUND
		var res byte
		if c == '?' {
			res = s.dst | 0177
		} else {
			res = s.dst | (c & 037)
		}
		s.dst = 0
		return res, UNVIS_VALID

	case S_OCTAL2: /* second possible octal digit */
		if IsOctal(c) {
			s.dst = (s.dst << 3) + (c - '0')
			s.astate = S_OCTAL3
			return 0, 0
		}
		s.astate = S_GROUND
		res := s.dst
		s.dst = 0
		return res, UNVIS_VALIDPUSH

	case S_OCTAL3: /* third possible octal digit */
		s.astate = S_GROUND
		if IsOctal(c) {
			res := (s.dst << 3) + (c - '0')
			s.dst = 0
			return res, UNVIS_VALID
		}
		res := s.dst
		s.dst = 0
		return res, UNVIS_VALIDPUSH

	default:
		s.astate = S_GROUND
		return 0, UNVIS_SYNBAD
	}
}

func BytesUnvis(dst, src []byte) ([]byte, error) {
	dst = dst[0:0]
	if len(src) == 0 {
		return dst, nil
	}
	var (
		c, res byte
		s      unvis
		flg    int
	)
	for _, c = range src {
	again:
		res, flg = s.unvis(c)
		switch flg {
		case UNVIS_VALID:
			dst = append(dst, res)
		case UNVIS_VALIDPUSH:
			dst = append(dst, res)
			goto again
		case 0, UNVIS_NOCHAR:
		default:
			return dst[0:0], errors.New("can't decode")
		}
	}
	if res, flg = s.end(); flg == UNVIS_VALID {
		dst = append(dst, res)
	}
	return dst, nil
}

func StreamUnvis(w io.Writer, r io.Reader) error {
	reader := bufio.NewReader(r)
	sbuf := make([]byte, 1)
	var (
		c, res byte
		s      unvis
		err    error
		flg    int
	)
	for {
		c, err = reader.ReadByte()
		if err == io.EOF {
			if res, flg = s.end(); flg == UNVIS_VALID {
				sbuf[0] = res
				_, err = w.Write(sbuf)
				return err
			}
			return nil
		}
		if err != nil {
			return err
		}
	again:
		res, flg = s.unvis(c)
		switch flg {
		case UNVIS_VALID:
			sbuf[0] = res
			_, err = w.Write(sbuf)
			if err != nil {
				return err
			}
		case UNVIS_VALIDPUSH:
			sbuf[0] = res
			_, err = w.Write(sbuf)
			if err != nil {
				return err
			}
			goto again
		case 0, UNVIS_NOCHAR:
		default:
			return errors.New("can't decode")
		}
	}
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
