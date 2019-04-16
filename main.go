package main

import (
	"os"

	"github.com/awnumar/memguard"
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
	startAgent()
	_ = app.Run(os.Args)
	stopAgent()
	cli.OsExiter(0)
}
