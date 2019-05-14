package format

import (
	"fmt"
	"github.com/ahmetb/go-linq"
	"github.com/logrusorgru/aurora"
	"github.com/stephane-martin/vssh/sys"
	"io"
	"sort"
	"strings"
)

func ListOfFiles(width int, long bool, files []sys.UFile, buf io.Writer) {
	// TODO: long should return more information
	maxlen := int(1)
	if len(files) != 0 {
		maxlen += linq.From(files).SelectT(func(info sys.UFile) int {
			if info.IsDir() {
				return len(info.Path) + 1
			}
			return len(info.Path)
		}).Max().(int)
	}
	padfmt := "%-" + fmt.Sprintf("%d", maxlen) + "s"
	columns := width / maxlen
	if columns == 0 {
		columns = 1
	}
	percolumn := (len(files) / columns) + 1

	sort.Slice(files, func(i, j int) bool {
		return files[i].Path < files[j].Path
	})
	aur := aurora.NewAurora(true)
	var name interface{}
	var lines [][]interface{}
	if long {
		lines = make([][]interface{}, len(files))
	} else {
		lines = make([][]interface{}, percolumn)
	}
	var line int
	for _, f := range files {
		line++
		if line > percolumn && !long {
			line = 1
		}
		if f.IsDir() {
			name = aur.Blue(fmt.Sprintf(padfmt, f.Path+"/"))
		} else if f.Mode().IsRegular() {
			if (f.Mode().Perm() & 0100) != 0 {
				name = aur.Green(fmt.Sprintf(padfmt, f.Path))
			} else {
				name = fmt.Sprintf(padfmt, f.Path)
			}
		} else {
			name = aur.Red(fmt.Sprintf(padfmt, f.Path))
		}
		if !strings.HasPrefix(f.Path, ".") {
			name = aur.Bold(name)
		}
		lines[line-1] = append(lines[line-1], name)
	}
	for _, line := range lines {
		for _, name := range line {
			_, _ = fmt.Fprint(buf, name)
		}
		_, _= fmt.Fprintln(buf)
	}
}
