package admin

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/ethereum-optimism/infra/op-signer/provider"
	oprpc "github.com/ethereum-optimism/optimism/op-service/rpc"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/rpc"
)

// MetricsInitFunc initializes placeholder request metrics for a configured key.
type MetricsInitFunc func(address, clientCN string)

// MetricsDeleteFunc deletes request metrics for a key being removed.
type MetricsDeleteFunc func(address, clientCN string)

type AdminService struct {
	logger log.Logger

	providerConfig  *provider.ProviderConfig
	keys            KeysProvider
	metricsInitFn   MetricsInitFunc
	metricsDeleteFn MetricsDeleteFunc
	mu              sync.Mutex
}

type metricLabelSet struct {
	address  string
	clientCN string
}

func NewAdminService(logger log.Logger, providerConfig *provider.ProviderConfig) (*AdminService, error) {
	return &AdminService{
		logger:         logger,
		providerConfig: providerConfig,
	}, nil
}

func (s *AdminService) SetKeysProvider(keys KeysProvider) {
	s.keys = keys
}

func (s *AdminService) SetMetricsInitFn(fn MetricsInitFunc) {
	s.metricsInitFn = fn
}

func (s *AdminService) SetMetricsDeleteFn(fn MetricsDeleteFunc) {
	s.metricsDeleteFn = fn
}

func collectMetricLabelCounts(auths []provider.AuthConfig) map[metricLabelSet]int {
	counts := make(map[metricLabelSet]int, len(auths))
	for _, authConfig := range auths {
		if authConfig.AllowedClientCN == "" {
			continue
		}

		labels := metricLabelSet{
			address:  authConfig.FromAddress.Hex(),
			clientCN: authConfig.AllowedClientCN,
		}
		counts[labels]++
	}
	return counts
}

func (s *AdminService) reconcileMetrics(before, after []provider.AuthConfig) {
	if s.metricsInitFn == nil && s.metricsDeleteFn == nil {
		return
	}

	beforeCounts := collectMetricLabelCounts(before)
	afterCounts := collectMetricLabelCounts(after)

	if s.metricsDeleteFn != nil {
		for labels := range beforeCounts {
			if afterCounts[labels] == 0 {
				s.metricsDeleteFn(labels.address, labels.clientCN)
			}
		}
	}

	if s.metricsInitFn != nil {
		for labels := range afterCounts {
			if beforeCounts[labels] == 0 {
				s.metricsInitFn(labels.address, labels.clientCN)
			}
		}
	}
}

func (s *AdminService) RegisterAPIs(server *oprpc.Server) {
	server.AddAPI(rpc.API{
		Namespace: "admin",
		Service:   s,
	})
}

func (s *AdminService) makeKeyConfig() []KeyConfig {
	auths := s.providerConfig.Auth()

	cfg := make([]KeyConfig, 0, len(auths))
	for _, authConfig := range auths {
		if authConfig.KeyName != "" {
			cfg = append(cfg, KeyConfig{
				AllowedClientCN: authConfig.AllowedClientCN,
				ParentChainID:   authConfig.ChainID,
				Path:            authConfig.KeyName,
			})
		}
	}
	return cfg
}

func (s *AdminService) GetConfigs(_ context.Context) ([]KeyConfig, error) {
	return s.makeKeyConfig(), nil
}

func (s *AdminService) tryAddPathPrefix(path string) string {
	pathRootPrefix := s.providerConfig.PathPrefix()
	if strings.HasPrefix(path, pathRootPrefix) {
		return path
	}

	return provider.MakeFullPath(pathRootPrefix, path)
}

func (s *AdminService) AddConfig(ctx context.Context, keyConfig KeyConfig) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.logger.Info("adding new key config",
		"path", keyConfig.Path,
		"chainId", keyConfig.ParentChainID,
		"allowedClientCN", keyConfig.AllowedClientCN)

	path := s.tryAddPathPrefix(keyConfig.Path)

	if res, err := s.GetConfigForPath(path); err == nil && res != nil {
		return "", fmt.Errorf("key already exists")
	}

	publicKey, err := s.keys.GetPublicKey(ctx, path)
	if err != nil {
		return "", fmt.Errorf("getting public key for path '%s': %w", path, err)
	}

	key, err := crypto.UnmarshalPubkey(publicKey)
	if err != nil {
		return "", fmt.Errorf("unmarshaling public key: %w", err)
	}

	if key == nil {
		return "", fmt.Errorf("unmarshaled public key is nil")
	}

	address := crypto.PubkeyToAddress(*key).Hex()

	newAuthConfig := provider.AuthConfig{
		AllowedClientCN: keyConfig.AllowedClientCN,
		ChainID:         keyConfig.ParentChainID,
		ClientName:      path,
		FromAddress:     common.HexToAddress(address),
		KeyName:         path,
		MaxValue:        "",
		ToAddresses:     nil,
	}

	before := s.providerConfig.Auth()
	s.providerConfig.AddConfig(address, newAuthConfig)
	s.reconcileMetrics(before, s.providerConfig.Auth())

	return address, nil
}

func (s *AdminService) RemoveConfigByPath(_ context.Context, path string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	resolvedPath := s.tryAddPathPrefix(path)

	before := s.providerConfig.Auth()
	s.providerConfig.RemoveConfigByPath(resolvedPath)
	s.reconcileMetrics(before, s.providerConfig.Auth())

	return path, nil
}

func (s *AdminService) RemoveConfig(_ context.Context, address string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	before := s.providerConfig.Auth()
	s.providerConfig.RemoveConfig(address)
	s.reconcileMetrics(before, s.providerConfig.Auth())

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

// ReloadKey reloads a key from Vault by path
func (s *AdminService) ReloadKey(ctx context.Context, path string) error {
	s.logger.Info("reloading key via admin API", "path", path)

	resolvedPath := s.tryAddPathPrefix(path)

	// Check if the provider supports key reloading
	reloader, ok := s.keys.(KeysReloader)
	if !ok {
		return rpc.HTTPError{StatusCode: 400, Status: "Bad Request", Body: []byte("provider does not support key reloading")}
	}

	if err := reloader.ReloadKey(ctx, resolvedPath); err != nil {
		s.logger.Error("failed to reload key", "path", resolvedPath, "error", err)
		return rpc.HTTPError{StatusCode: 500, Status: "ReloadKey Failed", Body: []byte(err.Error())}
	}

	s.logger.Info("successfully reloaded key via admin API", "path", resolvedPath)
	return nil
}
