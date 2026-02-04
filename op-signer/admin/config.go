package admin

import (
	oprpc "github.com/ethereum-optimism/optimism/op-service/rpc"
	optls "github.com/ethereum-optimism/optimism/op-service/tls"
)

type Config struct {
	Enabled   bool
	TLSConfig optls.CLIConfig
	RPCConfig oprpc.CLIConfig
}
