package admin

import (
	"context"
	"fmt"
	"sync"

	oprpc "github.com/ethereum-optimism/optimism/op-service/rpc"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/rpc"
)

type AdminService struct {
	logger log.Logger

	keyConfig map[string]*KeyConfig // address -> key config
	mu        sync.RWMutex          // protects keyConfig
}

func NewAdminService(logger log.Logger) (*AdminService, error) {
	return &AdminService{
		logger:    logger,
		keyConfig: make(map[string]*KeyConfig),
	}, nil
}

func (s *AdminService) RegisterAPIs(server *oprpc.Server) {
	server.AddAPI(rpc.API{
		Namespace: "admin",
		Service:   s,
	})
}

func (s *AdminService) GetConfigs(_ context.Context) (map[string]KeyConfig, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	configCopy := make(map[string]KeyConfig)
	for addr, cfg := range s.keyConfig {
		configCopy[addr] = *cfg
	}

	return configCopy, nil
}

func (s *AdminService) AddConfig(_ context.Context, address string, keyConfig KeyConfig) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.logger.Info("adding new key config",
		"address", address,
		"path", keyConfig.Path,
		"chainId", keyConfig.ParentChainID,
		"allowedClientCN", keyConfig.AllowedClientCN)

	if _, ok := s.keyConfig[keyConfig.AllowedClientCN]; ok {
		return "", fmt.Errorf("key already exists")
	}

	s.keyConfig[address] = &keyConfig

	return keyConfig.AllowedClientCN, nil
}

func (s *AdminService) RemoveConfig(_ context.Context, address string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.keyConfig[address]; !ok {
		return "", fmt.Errorf("key does not exist")
	}

	delete(s.keyConfig, address)

	return address, nil
}

func (s *AdminService) GetConfigForAddress(address string) (*KeyConfig, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	keyConfig, ok := s.keyConfig[address]
	if !ok {
		return nil, fmt.Errorf("key does not exist")
	}

	return keyConfig, nil
}

func (s *AdminService) GetConfigForPath(path string) (*KeyConfig, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	for _, keyConfig := range s.keyConfig {
		if keyConfig.Path == path {
			return keyConfig, nil
		}

	}

	return nil, fmt.Errorf("key does not exist")
}
