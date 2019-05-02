package wordwrap

import (
	"bytes"
	"unicode"

	runewidth "github.com/mattn/go-runewidth"
)

// WrapString wraps the given string within lim width in characters.
//
// Wrapping is currently naive and only happens at white-space. A future
// version of the library will implement smarter wrapping. This means that
// pathological cases can dramatically reach past the limit, such as a very
// long word.
func WrapString(s string, lim uint) string {
	// Initialize a buffer with a slightly larger size to account for breaks
	init := make([]byte, 0, len(s))
	buf := bytes.NewBuffer(init)

	var current uint
	var wordBuf, spaceBuf bytes.Buffer

	for _, char := range s {
		if char == '\n' {
			if bufLen(wordBuf) == 0 {
				if current+bufLen(spaceBuf) > lim {
					current = 0
				} else {
					current += bufLen(spaceBuf)
					spaceBuf.WriteTo(buf)
				}
				spaceBuf.Reset()
			} else {
				current += bufLen(spaceBuf) + bufLen(wordBuf)
				spaceBuf.WriteTo(buf)
				spaceBuf.Reset()
				wordBuf.WriteTo(buf)
				wordBuf.Reset()
			}
			buf.WriteRune(char)
			current = 0
		} else if unicode.IsSpace(char) {
			if bufLen(spaceBuf) == 0 || bufLen(wordBuf) > 0 {
				current += bufLen(spaceBuf) + bufLen(wordBuf)
				spaceBuf.WriteTo(buf)
				spaceBuf.Reset()
				wordBuf.WriteTo(buf)
				wordBuf.Reset()
			}

			spaceBuf.WriteRune(char)
		} else {

			l := uint(runewidth.RuneWidth(char))
			if current+bufLen(wordBuf)+l > lim {
				wordBuf.WriteTo(buf)
				buf.WriteRune('\n')
				current = 0
				spaceBuf.Reset()
				wordBuf.Reset()
			} else if current+bufLen(spaceBuf)+bufLen(wordBuf)+l > lim && bufLen(wordBuf)+l < lim {
				buf.WriteRune('\n')
				current = 0
				spaceBuf.Reset()
			}
			wordBuf.WriteRune(char)
		}
	}

	if wordBuf.Len() == 0 {
		if current+bufLen(spaceBuf) <= lim {
			spaceBuf.WriteTo(buf)
		}
	} else {
		spaceBuf.WriteTo(buf)
		wordBuf.WriteTo(buf)
	}

	return buf.String()
}

func bufLen(b bytes.Buffer) uint {
	return uint(runewidth.StringWidth(b.String()))
}
