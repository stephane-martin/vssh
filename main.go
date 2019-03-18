package main

import (
	"os"
	"time"

	"github.com/urfave/cli"
)

var Version string

func main() {
	app := App()
	app.Action = VSSH
	cli.OsExiter = func(code int) {
		os.Stdout.Sync()
		os.Stderr.Sync()
		if code != 0 {
			time.Sleep(200 * time.Millisecond)
			os.Exit(code)
		}
	}
	_ = app.Run(os.Args)
	cli.OsExiter(0)
}
