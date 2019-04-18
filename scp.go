package main

import "github.com/urfave/cli"

func scpCommand() cli.Command {
	return cli.Command{
		Name:  "scp",
		Usage: "download/upload files with scp protocol using Vault for authentication",
		Subcommands: []cli.Command{
			scpPutCommand(),
			scpGetCommand(),
		},
	}
}
