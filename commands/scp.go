package commands

import "github.com/urfave/cli"

func SCPCommand() cli.Command {
	return cli.Command{
		Name:  "scp",
		Usage: "download/upload files with scp protocol using Vault for authentication",
		Subcommands: []cli.Command{
			SCPPutCommand(),
			SCPGetCommand(),
		},
	}
}
