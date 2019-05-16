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
	tview.Styles = tview.Theme{
		PrimitiveBackgroundColor:    tcell.ColorBlack,
		ContrastBackgroundColor:     tcell.ColorDarkSlateGray,
		MoreContrastBackgroundColor: tcell.ColorDarkCyan,
		BorderColor:                 tcell.ColorLightYellow,
		TitleColor:                  tcell.ColorViolet,
		GraphicsColor:               tcell.ColorWhite,
		PrimaryTextColor:            tcell.ColorFloralWhite,
		SecondaryTextColor:          tcell.ColorLightBlue,
		TertiaryTextColor:           tcell.ColorLightGreen,
		InverseTextColor:            tcell.ColorBlue,
		ContrastSecondaryTextColor:  tcell.ColorLightCoral,
	}

}
