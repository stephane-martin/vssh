package main

import (
	"os"

	"github.com/stephane-martin/vssh/sys"

	"github.com/awnumar/memguard"
	"github.com/gdamore/tcell"
	"github.com/rivo/tview"
	"github.com/urfave/cli"
)

// version stores the current version number of vssh. It it set by the Makefile.
var version string

func main() {
	app := App()
	cli.OsExiter = func(code int) {
		_ = os.Stdout.Sync()
		_ = os.Stderr.Sync()
		memguard.DestroyAll()
		if code != 0 {
			os.Exit(code)
		}
	}
	sys.StartAgent()
	_ = app.Run(os.Args)
	sys.StopAgent()
	cli.OsExiter(0)
}

func init() {
	tview.Styles.PrimitiveBackgroundColor = tcell.ColorDarkSlateGrey
	tview.Styles.ContrastBackgroundColor = tcell.ColorSlateGrey
	tview.Styles.PrimaryTextColor = tcell.ColorWhite
}
