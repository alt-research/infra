package admin

import (
	"context"
	"fmt"
	"strings"

	"github.com/ethereum-optimism/infra/op-signer/provider"
	oprpc "github.com/ethereum-optimism/optimism/op-service/rpc"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/rpc"
)

type AdminService struct {
	logger log.Logger

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

func (s *AdminService) makeKeyConfig() map[string]KeyConfig {
	auths := s.providerConfig.Auth()

	cfg := make(map[string]KeyConfig, len(auths))
	for _, authConfig := range auths {
		cfg[authConfig.FromAddress.Hex()] = KeyConfig{
			AllowedClientCN: authConfig.AllowedClientCN,
			ParentChainID:   authConfig.ChainID,
			Path:            authConfig.KeyName,
		}
	}
	return cfg
}

func (s *AdminService) GetConfigs(_ context.Context) (map[string]KeyConfig, error) {
	return s.makeKeyConfig(), nil
}

func (s *AdminService) tryAddPathPrefix(path string) string {
	pathRootPrefix := s.providerConfig.PathPrefix()
	if strings.HasPrefix(path, pathRootPrefix) {
		return path
	}

	return provider.MakeFullPath(pathRootPrefix, path)
}

func (s *AdminService) AddConfig(_ context.Context, address string, keyConfig KeyConfig) (string, error) {
	s.logger.Info("adding new key config",
		"address", address,
		"path", keyConfig.Path,
		"chainId", keyConfig.ParentChainID,
		"allowedClientCN", keyConfig.AllowedClientCN)

	path := s.tryAddPathPrefix(keyConfig.Path)

	if res, err := s.GetConfigForPath(path); err == nil && res != nil {
		return "", fmt.Errorf("key already exists")
	}

	newAuthConfig := provider.AuthConfig{
		AllowedClientCN: keyConfig.AllowedClientCN,
		ChainID:         keyConfig.ParentChainID,
		ClientName:      path,
		FromAddress:     common.HexToAddress(address),
		KeyName:         path,
		MaxValue:        "",
		ToAddresses:     nil,
	}

	s.providerConfig.AddConfig(address, newAuthConfig)

	return keyConfig.AllowedClientCN, nil
}

func (s *AdminService) RemoveConfig(_ context.Context, address string) (string, error) {
	s.providerConfig.RemoveConfig(address)

	return address, nil
}

func (s *AdminService) GetConfigForAddress(address string) (*KeyConfig, error) {
	authConfig, err := s.providerConfig.GetConfigByAddress(address)
	if err != nil {
		return nil, err
	}

	return &KeyConfig{
		AllowedClientCN: authConfig.AllowedClientCN,
		ParentChainID:   authConfig.ChainID,
		Path:            authConfig.KeyName,
	}, nil
}

func (s *AdminService) GetConfigForPath(path string) (*KeyConfig, error) {
	path = s.tryAddPathPrefix(path)
	authConfig, err := s.providerConfig.GetConfigByPath(path)

	if err != nil {
		return nil, err
	}

	return &KeyConfig{
		AllowedClientCN: authConfig.AllowedClientCN,
		ParentChainID:   authConfig.ChainID,
		Path:            authConfig.KeyName,
	}, nil
}
