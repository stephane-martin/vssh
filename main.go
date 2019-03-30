package main

import (
	"github.com/awnumar/memguard"
	"github.com/urfave/cli"
	"os"
)

// Version stores the current version number of vssh. It it set by the Makefile.
var Version string

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
	_ = app.Run(os.Args)
	cli.OsExiter(0)
}
