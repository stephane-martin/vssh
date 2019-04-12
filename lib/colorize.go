package lib

import (
	"errors"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"

	"github.com/alecthomas/chroma/formatters"
	"github.com/alecthomas/chroma/lexers"
	"github.com/alecthomas/chroma/styles"
	"github.com/rivo/tview"
)

func Colorize(name string, text io.Reader, out io.Writer) error {
	ext := strings.ToLower(filepath.Ext(name))
	if ext == ".pdf" {
		return PDFToText(text, out)
	}
	lexer := lexers.Match(filepath.Base(name))
	if lexer == nil {
		_, _ = io.Copy(out, text)
		return errors.New("lexer not found")
	}
	styleName := os.Getenv("VSSH_THEME")
	if styleName == "" {
		styleName = "monokai"
	}
	style := styles.Get(styleName)
	if style == nil {
		_, _ = io.Copy(out, text)
		return errors.New("style not found")
	}
	formatter := formatters.Get("terminal256")
	if formatter == nil {
		_, _ = io.Copy(out, text)
		return errors.New("formatter not found")
	}
	t, err := ioutil.ReadAll(text)
	if err != nil {
		return err
	}
	iterator, err := lexer.Tokenise(nil, string(t))
	if err != nil {
		_, _ = out.Write(t)
		return err
	}
	if box, ok := out.(*tview.TextView); ok {
		box.SetDynamicColors(true)
		out = tview.ANSIWriter(out)
	}
	return formatter.Format(out, style, iterator)
}
