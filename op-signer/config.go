package app

import (
	"fmt"

	"github.com/urfave/cli/v2"

	opservice "github.com/ethereum-optimism/optimism/op-service"
	oplog "github.com/ethereum-optimism/optimism/op-service/log"
	opmetrics "github.com/ethereum-optimism/optimism/op-service/metrics"
	"github.com/ethereum-optimism/optimism/op-service/oppprof"
	oprpc "github.com/ethereum-optimism/optimism/op-service/rpc"
	optls "github.com/ethereum-optimism/optimism/op-service/tls"

	"github.com/ethereum-optimism/infra/op-signer/admin"
)

const (
	ServiceConfigPathFlagName = "config"
	ClientEndpointFlagName    = "endpoint"
)

func CLIFlags(envPrefix string) []cli.Flag {
	flags := []cli.Flag{
		&cli.StringFlag{
			Name:    ServiceConfigPathFlagName,
			Usage:   "Signer service configuration file path",
			Value:   "config.yaml",
			EnvVars: opservice.PrefixEnvVar(envPrefix, "SERVICE_CONFIG"),
		},
	}
	flags = append(flags, oprpc.CLIFlags(envPrefix)...)
	// Add admin RPC flags with custom prefixes
	adminFlags := []cli.Flag{
		&cli.StringFlag{
			Name:     "admin.rpc.addr",
			Usage:    "admin rpc listening address",
			Value:    "0.0.0.0",
			EnvVars:  opservice.PrefixEnvVar(fmt.Sprintf("%s_ADMIN", envPrefix), "RPC_ADDR"),
			Category: "ADMIN",
		},
		&cli.IntFlag{
			Name:     "admin.rpc.port",
			Usage:    "admin rpc listening port",
			Value:    9545, // Default admin port
			EnvVars:  opservice.PrefixEnvVar(fmt.Sprintf("%s_ADMIN", envPrefix), "RPC_PORT"),
			Category: "ADMIN",
		},
		&cli.BoolFlag{
			Name:     "admin.rpc.enable-admin",
			Usage:    "Enable the admin API for admin server",
			Value:    false,
			EnvVars:  opservice.PrefixEnvVar(fmt.Sprintf("%s_ADMIN", envPrefix), "RPC_ENABLE_ADMIN"),
			Category: "ADMIN",
		},
		&cli.BoolFlag{
			Name:     "admin.tls.enabled",
			Usage:    "Enable TLS for admin server",
			Value:    false,
			EnvVars:  opservice.PrefixEnvVar(fmt.Sprintf("%s_ADMIN", envPrefix), "TLS_ENABLED"),
			Category: "ADMIN",
		},
		&cli.StringFlag{
			Name:     "admin.tls.cert",
			Usage:    "TLS certificate path for admin server",
			EnvVars:  opservice.PrefixEnvVar(fmt.Sprintf("%s_ADMIN", envPrefix), "TLS_CERT"),
			Category: "ADMIN",
		},
		&cli.StringFlag{
			Name:     "admin.tls.key",
			Usage:    "TLS private key path for admin server",
			EnvVars:  opservice.PrefixEnvVar(fmt.Sprintf("%s_ADMIN", envPrefix), "TLS_KEY"),
			Category: "ADMIN",
		},
		&cli.StringFlag{
			Name:     "admin.tls.ca",
			Usage:    "TLS CA certificate path for admin server",
			EnvVars:  opservice.PrefixEnvVar(fmt.Sprintf("%s_ADMIN", envPrefix), "TLS_CA"),
			Category: "ADMIN",
		},
	}
	flags = append(flags, adminFlags...) // Add admin RPC flags
	flags = append(flags, oplog.CLIFlags(envPrefix)...)
	flags = append(flags, opmetrics.CLIFlags(envPrefix)...)
	flags = append(flags, oppprof.CLIFlags(envPrefix)...)
	flags = append(flags, optls.CLIFlags(envPrefix)...)
	// Admin TLS flags are now included in adminFlags section
	return flags
}

type Config struct {
	ClientEndpoint    string
	ServiceConfigPath string

	TLSConfig     optls.CLIConfig
	RPCConfig     oprpc.CLIConfig
	LogConfig     oplog.CLIConfig
	MetricsConfig opmetrics.CLIConfig
	PprofConfig   oppprof.CLIConfig

	AdminConfig admin.Config
}

func (c Config) Check() error {
	if err := c.RPCConfig.Check(); err != nil {
		return err
	}
	if err := c.MetricsConfig.Check(); err != nil {
		return err
	}
	if err := c.PprofConfig.Check(); err != nil {
		return err
	}
	if err := c.TLSConfig.Check(); err != nil {
		return err
	}
	if err := c.AdminConfig.RPCConfig.Check(); err != nil {
		return err
	}
	return nil
}

func NewConfig(ctx *cli.Context) *Config {
	return &Config{
		ClientEndpoint:    ctx.String(ClientEndpointFlagName),
		ServiceConfigPath: ctx.String(ServiceConfigPathFlagName),
		TLSConfig:         optls.ReadCLIConfigWithPrefix(ctx, ""),
		RPCConfig:         oprpc.ReadCLIConfig(ctx),
		LogConfig:         oplog.ReadCLIConfig(ctx),
		MetricsConfig:     opmetrics.ReadCLIConfig(ctx),
		PprofConfig:       oppprof.ReadCLIConfig(ctx),
		AdminConfig: admin.Config{
			Enabled: ctx.Bool("admin.tls.enabled"),
			RPCConfig: oprpc.CLIConfig{
				ListenAddr:  ctx.String("admin.rpc.addr"),
				ListenPort:  ctx.Int("admin.rpc.port"),
				EnableAdmin: ctx.Bool("admin.rpc.enable-admin"),
			},
			TLSConfig: optls.CLIConfig{
				Enabled:   ctx.Bool("admin.tls.enabled"),
				TLSCaCert: ctx.String("admin.tls.ca"),
				TLSCert:   ctx.String("admin.tls.cert"),
				TLSKey:    ctx.String("admin.tls.key"),
			},
		},
	}
}
