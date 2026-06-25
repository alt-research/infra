package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/urfave/cli/v2"

	"github.com/ethereum/go-ethereum/log"

	signer "github.com/ethereum-optimism/infra/op-signer"
	"github.com/ethereum-optimism/infra/op-signer/admin"
	"github.com/ethereum-optimism/optimism/op-service/cliapp"
	oplog "github.com/ethereum-optimism/optimism/op-service/log"
	clsigner "github.com/ethereum-optimism/optimism/op-service/signer"
)

var (
	Version   = ""
	GitCommit = ""
	GitDate   = ""
)

func printJSON(v any) error {
	out, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling output: %w", err)
	}
	fmt.Println(string(out))
	return nil
}

func main() {
	oplog.SetupDefaults()

	app := cli.NewApp()
	app.Flags = cliapp.ProtectFlags(signer.CLIFlags("OP_SIGNER"))
	app.Version = fmt.Sprintf("%s-%s-%s", Version, GitCommit, GitDate)
	app.Name = "op-signer"
	app.Usage = "OP Signing Service"
	app.Description = ""
	app.Commands = []*cli.Command{
		{
			Name:  "client",
			Usage: "test client for signer service",
			Subcommands: []*cli.Command{
				{
					Name:   string(signer.SignTransaction),
					Usage:  "sign a transaction, 1 arg: a hex-encoded tx",
					Action: signer.ClientSign(signer.SignTransaction),
					Flags:  cliapp.ProtectFlags(clsigner.CLIFlags("OP_SIGNER", "CLIENT")),
				},
				{
					Name:   string(signer.SignBlockPayload),
					Usage:  "sign a block payload using V1 API, 3 args: payloadHash, chainID, domain",
					Action: signer.ClientSign(signer.SignBlockPayload),
					Flags:  cliapp.ProtectFlags(clsigner.CLIFlags("OP_SIGNER", "CLIENT")),
				},
				{
					Name:   string(signer.SignBlockPayloadV2),
					Usage:  "sign a block payload using V2 API, 3 args: payloadHash, chainID, domain",
					Action: signer.ClientSign(signer.SignBlockPayloadV2),
					Flags:  cliapp.ProtectFlags(clsigner.CLIFlags("OP_SIGNER", "CLIENT")),
				},
			},
		},
		{
			Name:  "get-config",
			Usage: "list all key configurations and their derived Ethereum addresses",
			Flags: []cli.Flag{
				&cli.StringFlag{
					Name:    "admin.endpoint",
					Usage:   "Admin API endpoint URL",
					Value:   "http://localhost:8546",
					EnvVars: []string{"OP_SIGNER_ADMIN_ENDPOINT"},
				},
			},
			Action: func(ctx *cli.Context) error {
				client := admin.NewAdminClient(ctx.String("admin.endpoint"))
				configs, err := client.GetConfigs()
				if err != nil {
					return fmt.Errorf("getting configs: %w", err)
				}
				return printJSON(configs)
			},
		},
	}

	app.Action = cliapp.LifecycleCmd(signer.MainAppAction(Version))
	err := app.Run(os.Args)
	if err != nil {
		log.Crit("Application failed", "message", err)
	}
}
