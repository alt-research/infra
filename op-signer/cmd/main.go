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

var adminFlags = []cli.Flag{
	&cli.StringFlag{
		Name:    "admin.endpoint",
		Usage:   "Admin API endpoint URL",
		Value:   "https://localhost:9545",
		EnvVars: []string{"OP_SIGNER_ADMIN_ENDPOINT"},
	},
	&cli.StringFlag{
		Name:    "admin.password",
		Usage:   "Admin API password for basic auth",
		EnvVars: []string{"OP_SIGNER_ADMIN_PASSWORD"},
	},
	&cli.StringFlag{
		Name:    "admin.tls.cert",
		Usage:   "Client TLS certificate path",
		EnvVars: []string{"OP_SIGNER_ADMIN_TLS_CERT"},
	},
	&cli.StringFlag{
		Name:    "admin.tls.key",
		Usage:   "Client TLS private key path",
		EnvVars: []string{"OP_SIGNER_ADMIN_TLS_KEY"},
	},
	&cli.StringFlag{
		Name:    "admin.tls.ca",
		Usage:   "CA certificate path",
		EnvVars: []string{"OP_SIGNER_ADMIN_TLS_CA"},
	},
}

func newAdminClient(ctx *cli.Context) (*admin.AdminClient, error) {
	password := ctx.String("admin.password")
	if password == "" {
		return nil, fmt.Errorf("--admin.password is required")
	}
	return admin.NewAdminClient(
		ctx.String("admin.endpoint"),
		password,
		ctx.String("admin.tls.cert"),
		ctx.String("admin.tls.key"),
		ctx.String("admin.tls.ca"),
	)
}

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
			Name:  "admin",
			Usage: "admin CLI for managing op-signer configurations",
			Flags: adminFlags,
			Subcommands: []*cli.Command{
				{
					Name:  "get-configs",
					Usage: "get all key configurations",
					Action: func(ctx *cli.Context) error {
						client, err := newAdminClient(ctx)
						if err != nil {
							return err
						}
						configs, err := client.GetConfigs()
						if err != nil {
							return fmt.Errorf("getting configs: %w", err)
						}
						return printJSON(configs)
					},
				},
				{
					Name:  "add-config",
					Usage: "add a key configuration for an address",
					Flags: []cli.Flag{
						&cli.StringFlag{
							Name:     "address",
							Usage:    "address or key identifier",
							Required: true,
						},
						&cli.StringFlag{
							Name:     "path",
							Usage:    "vault path to the key",
							Required: true,
						},
						&cli.Uint64Flag{
							Name:  "parent-chain-id",
							Usage: "parent chain ID for the key",
						},
						&cli.StringFlag{
							Name:  "allowed-client-cn",
							Usage: "allowed client certificate common name",
						},
					},
					Action: func(ctx *cli.Context) error {
						client, err := newAdminClient(ctx)
						if err != nil {
							return err
						}
						keyConfig := admin.KeyConfig{
							Path:            ctx.String("path"),
							ParentChainID:   ctx.Uint64("parent-chain-id"),
							AllowedClientCN: ctx.String("allowed-client-cn"),
						}
						result, err := client.AddConfig(ctx.String("address"), keyConfig)
						if err != nil {
							return fmt.Errorf("adding config: %w", err)
						}
						return printJSON(result)
					},
				},
				{
					Name:  "remove-config",
					Usage: "remove a key configuration by address",
					Flags: []cli.Flag{
						&cli.StringFlag{
							Name:     "address",
							Usage:    "address to remove",
							Required: true,
						},
					},
					Action: func(ctx *cli.Context) error {
						client, err := newAdminClient(ctx)
						if err != nil {
							return err
						}
						result, err := client.RemoveConfig(ctx.String("address"))
						if err != nil {
							return fmt.Errorf("removing config: %w", err)
						}
						return printJSON(result)
					},
				},
				{
					Name:  "get-config-by-address",
					Usage: "get key configuration for a specific address",
					Flags: []cli.Flag{
						&cli.StringFlag{
							Name:     "address",
							Usage:    "address to look up",
							Required: true,
						},
					},
					Action: func(ctx *cli.Context) error {
						client, err := newAdminClient(ctx)
						if err != nil {
							return err
						}
						config, err := client.GetConfigForAddress(ctx.String("address"))
						if err != nil {
							return fmt.Errorf("getting config by address: %w", err)
						}
						return printJSON(config)
					},
				},
				{
					Name:  "get-config-by-path",
					Usage: "get key configuration for a specific vault path",
					Flags: []cli.Flag{
						&cli.StringFlag{
							Name:     "path",
							Usage:    "vault path to look up",
							Required: true,
						},
					},
					Action: func(ctx *cli.Context) error {
						client, err := newAdminClient(ctx)
						if err != nil {
							return err
						}
						config, err := client.GetConfigForPath(ctx.String("path"))
						if err != nil {
							return fmt.Errorf("getting config by path: %w", err)
						}
						return printJSON(config)
					},
				},
			},
		},
	}

	app.Action = cliapp.LifecycleCmd(signer.MainAppAction(Version))
	err := app.Run(os.Args)
	if err != nil {
		log.Crit("Application failed", "message", err)
	}
}
