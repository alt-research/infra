package admin

import (
	"context"

	"github.com/ethereum-optimism/infra/op-signer/provider"
	oprpc "github.com/ethereum-optimism/optimism/op-service/rpc"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/rpc"
)

type AdminService struct {
	logger         log.Logger
	providerConfig *provider.ProviderConfig
}

func NewAdminService(logger log.Logger, providerConfig *provider.ProviderConfig) (*AdminService, error) {
	return &AdminService{
		logger:         logger,
		providerConfig: providerConfig,
	}, nil
}

func (s *AdminService) RegisterAPIs(server *oprpc.Server) {
	server.AddAPI(rpc.API{
		Namespace: "admin",
		Service:   s,
	})
}

// GetConfigs returns all key configurations including the derived Ethereum address for each key.
func (s *AdminService) GetConfigs(_ context.Context) ([]KeyConfig, error) {
	auths := s.providerConfig.Auth()

	cfg := make([]KeyConfig, 0, len(auths))
	for _, authConfig := range auths {
		if authConfig.KeyName != "" {
			cfg = append(cfg, KeyConfig{
				Address:         authConfig.FromAddress.Hex(),
				AllowedClientCN: authConfig.AllowedClientCN,
				ParentChainID:   authConfig.ChainID,
				Path:            authConfig.KeyName,
			})
		}
	}
	return cfg, nil
}
