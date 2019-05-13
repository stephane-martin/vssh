package widgets

import (
	"errors"
	"github.com/alecthomas/chroma/formatters"
	"github.com/alecthomas/chroma/lexers"
	"github.com/alecthomas/chroma/styles"
	"github.com/rivo/tview"
	"github.com/stephane-martin/vssh/textconv"
	"io"
	"os"
	"path/filepath"
	"strings"
)

func Colorize(name string, content []byte, out io.Writer) error {
	ext := strings.ToLower(filepath.Ext(name))
	if ext == ".pdf" {
		return textconv.PDFToText(content, out)
	} else if ext == ".docx" {
		return textconv.ConvertDocx(content, out)
	}
	if textconv.IsBinary(content) {
		return errors.New("looks like binary")
	}
	lexer := lexers.Match(filepath.Base(name))
	if lexer == nil {
		_, err := out.Write(content)
		return err
	}
	styleName := os.Getenv("VSSH_THEME")
	if styleName == "" {
		styleName = "monokai"
	}
	style := styles.Get(styleName)
	if style == nil {
		return errors.New("style not found")
	}
	formatter := formatters.Get("terminal256")
	if formatter == nil {
		return errors.New("formatter not found")
	}
	iterator, err := lexer.Tokenise(nil, string(content))
	if err != nil {
		return err
	}
	if box, ok := out.(*tview.TextView); ok {
		box.SetDynamicColors(true)
		out = tview.ANSIWriter(out)
	}
	return formatter.Format(out, style, iterator)
}

