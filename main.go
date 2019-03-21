package main

import (
	"os"
	"time"

	"github.com/awnumar/memguard"
	"github.com/urfave/cli"
)

// Version stores the current version number of vssh. It it set by the Makefile.
var Version string

func main() {
	app := App()
	app.Action = VSSH
	cli.OsExiter = func(code int) {
		os.Stdout.Sync()
		os.Stderr.Sync()
		memguard.DestroyAll()
		time.Sleep(200 * time.Millisecond)
		if code != 0 {
			os.Exit(code)
		}
	}
	_ = app.Run(os.Args)
	cli.OsExiter(0)
}
