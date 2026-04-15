package admin

import (
	"context"
	"fmt"
	"testing"

	"github.com/ethereum-optimism/infra/op-signer/provider"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/log"
	"github.com/stretchr/testify/require"
)

type stubKeysProvider struct {
	publicKey  []byte
	publicKeys map[string][]byte
}

func (s stubKeysProvider) GetPublicKey(_ context.Context, keyName string) ([]byte, error) {
	if s.publicKeys != nil {
		publicKey, ok := s.publicKeys[keyName]
		if !ok {
			return nil, fmt.Errorf("missing public key for %s", keyName)
		}
		return publicKey, nil
	}

	return s.publicKey, nil
}

func TestAddConfigSkipsMetricsInitWhenAllowedClientCNIsEmpty(t *testing.T) {
	publicKey, expectedAddress := newTestPublicKey(t)

	service, err := NewAdminService(log.New(), &provider.ProviderConfig{})
	require.NoError(t, err)
	service.SetKeysProvider(stubKeysProvider{publicKey: publicKey})

	initCalls := 0
	service.SetMetricsInitFn(func(address, clientCN string) {
		initCalls++
	})

	address, err := service.AddConfig(context.Background(), KeyConfig{
		Path:          "test-key-empty-cn",
		ParentChainID: 1,
	})
	require.NoError(t, err)
	require.Equal(t, expectedAddress, address)
	require.Zero(t, initCalls)
}

func TestAddConfigInitializesMetricsWhenAllowedClientCNIsConfigured(t *testing.T) {
	publicKey, expectedAddress := newTestPublicKey(t)

	service, err := NewAdminService(log.New(), &provider.ProviderConfig{})
	require.NoError(t, err)
	service.SetKeysProvider(stubKeysProvider{publicKey: publicKey})

	initCalls := 0
	var gotAddress string
	var gotClientCN string
	service.SetMetricsInitFn(func(address, clientCN string) {
		initCalls++
		gotAddress = address
		gotClientCN = clientCN
	})

	address, err := service.AddConfig(context.Background(), KeyConfig{
		Path:            "test-key-restricted-cn",
		ParentChainID:   1,
		AllowedClientCN: "batcher-1",
	})
	require.NoError(t, err)
	require.Equal(t, expectedAddress, address)
	require.Equal(t, 1, initCalls)
	require.Equal(t, expectedAddress, gotAddress)
	require.Equal(t, "batcher-1", gotClientCN)
}

func TestRemoveConfigCallsDeleteCallback(t *testing.T) {
	publicKey, expectedAddress := newTestPublicKey(t)

	service, err := NewAdminService(log.New(), &provider.ProviderConfig{})
	require.NoError(t, err)
	service.SetKeysProvider(stubKeysProvider{publicKey: publicKey})

	// Add a config first
	_, err = service.AddConfig(context.Background(), KeyConfig{
		Path:            "test-key-to-remove",
		ParentChainID:   1,
		AllowedClientCN: "challenger-1",
	})
	require.NoError(t, err)

	// Set up delete callback
	deleteCalls := 0
	var gotAddress string
	var gotClientCN string
	service.SetMetricsDeleteFn(func(address, clientCN string) {
		deleteCalls++
		gotAddress = address
		gotClientCN = clientCN
	})

	// Remove the config
	removedAddress, err := service.RemoveConfig(context.Background(), expectedAddress)
	require.NoError(t, err)
	require.Equal(t, expectedAddress, removedAddress)
	require.Equal(t, 1, deleteCalls)
	require.Equal(t, expectedAddress, gotAddress)
	require.Equal(t, "challenger-1", gotClientCN)
}

func TestRemoveConfigSkipsDeleteCallbackWhenNoClientCN(t *testing.T) {
	publicKey, expectedAddress := newTestPublicKey(t)

	service, err := NewAdminService(log.New(), &provider.ProviderConfig{})
	require.NoError(t, err)
	service.SetKeysProvider(stubKeysProvider{publicKey: publicKey})

	// Add a config without AllowedClientCN
	_, err = service.AddConfig(context.Background(), KeyConfig{
		Path:          "test-key-no-cn",
		ParentChainID: 1,
	})
	require.NoError(t, err)

	// Set up delete callback
	deleteCalls := 0
	service.SetMetricsDeleteFn(func(address, clientCN string) {
		deleteCalls++
	})

	// Remove the config - callback should not be called since there's no clientCN
	_, err = service.RemoveConfig(context.Background(), expectedAddress)
	require.NoError(t, err)
	require.Zero(t, deleteCalls)
}

func TestRemoveConfigByPathCallsDeleteCallback(t *testing.T) {
	publicKey, _ := newTestPublicKey(t)

	service, err := NewAdminService(log.New(), &provider.ProviderConfig{})
	require.NoError(t, err)
	service.SetKeysProvider(stubKeysProvider{publicKey: publicKey})

	// Add a config first
	_, err = service.AddConfig(context.Background(), KeyConfig{
		Path:            "test-key-path-remove",
		ParentChainID:   1,
		AllowedClientCN: "sequencer-1",
	})
	require.NoError(t, err)

	// Set up delete callback
	deleteCalls := 0
	service.SetMetricsDeleteFn(func(address, clientCN string) {
		deleteCalls++
	})

	// Remove by path
	_, err = service.RemoveConfigByPath(context.Background(), "test-key-path-remove")
	require.NoError(t, err)
	require.Equal(t, 1, deleteCalls)
}

func TestRemoveConfigDeletesMetricsForAllRemovedClientCNsOnSharedAddress(t *testing.T) {
	publicKey, expectedAddress := newTestPublicKey(t)

	service, err := NewAdminService(log.New(), &provider.ProviderConfig{})
	require.NoError(t, err)
	service.SetKeysProvider(stubKeysProvider{
		publicKeys: map[string][]byte{
			"challenger-a": publicKey,
			"challenger-b": publicKey,
		},
	})

	_, err = service.AddConfig(context.Background(), KeyConfig{
		Path:            "challenger-a",
		ParentChainID:   1,
		AllowedClientCN: "arena-z-testnet-op-challenger",
	})
	require.NoError(t, err)

	_, err = service.AddConfig(context.Background(), KeyConfig{
		Path:            "challenger-b",
		ParentChainID:   1,
		AllowedClientCN: "arena-z-testnet-seq-challenger",
	})
	require.NoError(t, err)

	var deleted []string
	service.SetMetricsDeleteFn(func(address, clientCN string) {
		deleted = append(deleted, address+"|"+clientCN)
	})

	removedAddress, err := service.RemoveConfig(context.Background(), expectedAddress)
	require.NoError(t, err)
	require.Equal(t, expectedAddress, removedAddress)
	require.ElementsMatch(t, []string{
		expectedAddress + "|arena-z-testnet-op-challenger",
		expectedAddress + "|arena-z-testnet-seq-challenger",
	}, deleted)
}

func TestRemoveConfigByPathOnlyDeletesMetricWhenLastMatchingConfigIsRemoved(t *testing.T) {
	publicKey, expectedAddress := newTestPublicKey(t)

	providerConfig := &provider.ProviderConfig{}
	providerConfig.SetPathPrefix("test-prefix")

	service, err := NewAdminService(log.New(), providerConfig)
	require.NoError(t, err)
	service.SetKeysProvider(stubKeysProvider{
		publicKeys: map[string][]byte{
			"test-prefix/key-a": publicKey,
			"test-prefix/key-b": publicKey,
		},
	})

	initCalls := 0
	service.SetMetricsInitFn(func(address, clientCN string) {
		initCalls++
		require.Equal(t, expectedAddress, address)
		require.Equal(t, "shared-client", clientCN)
	})

	_, err = service.AddConfig(context.Background(), KeyConfig{
		Path:            "key-a",
		ParentChainID:   1,
		AllowedClientCN: "shared-client",
	})
	require.NoError(t, err)

	_, err = service.AddConfig(context.Background(), KeyConfig{
		Path:            "key-b",
		ParentChainID:   1,
		AllowedClientCN: "shared-client",
	})
	require.NoError(t, err)
	require.Equal(t, 1, initCalls)

	var deleted []string
	service.SetMetricsDeleteFn(func(address, clientCN string) {
		deleted = append(deleted, address+"|"+clientCN)
	})

	removedPath, err := service.RemoveConfigByPath(context.Background(), "key-a")
	require.NoError(t, err)
	require.Equal(t, "key-a", removedPath)
	require.Empty(t, deleted)

	removedPath, err = service.RemoveConfigByPath(context.Background(), "key-b")
	require.NoError(t, err)
	require.Equal(t, "key-b", removedPath)
	require.Equal(t, []string{expectedAddress + "|shared-client"}, deleted)
}

func newTestPublicKey(t *testing.T) ([]byte, string) {
	t.Helper()

	key, err := crypto.GenerateKey()
	require.NoError(t, err)

	return crypto.FromECDSAPub(&key.PublicKey), crypto.PubkeyToAddress(key.PublicKey).Hex()
}

// mockKeysReloader is a mock KeysProvider that also implements KeysReloader
type mockKeysReloader struct {
	stubKeysProvider
	reloadKeyCalls int
	reloadKeyError error
	reloadKeyName  string
}

func (m *mockKeysReloader) ReloadKey(_ context.Context, keyName string) error {
	m.reloadKeyCalls++
	m.reloadKeyName = keyName
	return m.reloadKeyError
}

func TestReloadKeySuccess(t *testing.T) {
	publicKey, _ := newTestPublicKey(t)

	service, err := NewAdminService(log.New(), &provider.ProviderConfig{})
	require.NoError(t, err)

	mock := &mockKeysReloader{
		stubKeysProvider: stubKeysProvider{publicKey: publicKey},
	}
	service.SetKeysProvider(mock)

	err = service.ReloadKey(context.Background(), "test-key")
	require.NoError(t, err)
	require.Equal(t, 1, mock.reloadKeyCalls)
	require.Equal(t, "test-key", mock.reloadKeyName)
}

func TestReloadKeyWithPathPrefix(t *testing.T) {
	publicKey, _ := newTestPublicKey(t)

	providerConfig := &provider.ProviderConfig{}
	providerConfig.SetPathPrefix("vault/path")

	service, err := NewAdminService(log.New(), providerConfig)
	require.NoError(t, err)

	mock := &mockKeysReloader{
		stubKeysProvider: stubKeysProvider{publicKey: publicKey},
	}
	service.SetKeysProvider(mock)

	err = service.ReloadKey(context.Background(), "my-key")
	require.NoError(t, err)
	require.Equal(t, 1, mock.reloadKeyCalls)
	require.Equal(t, "vault/path/my-key", mock.reloadKeyName)
}

func TestReloadKeyProviderNotSupportReloader(t *testing.T) {
	publicKey, _ := newTestPublicKey(t)

	service, err := NewAdminService(log.New(), &provider.ProviderConfig{})
	require.NoError(t, err)

	// Use stubKeysProvider which does NOT implement KeysReloader
	service.SetKeysProvider(stubKeysProvider{publicKey: publicKey})

	err = service.ReloadKey(context.Background(), "test-key")
	require.Error(t, err)
	require.Contains(t, err.Error(), "provider does not support key reloading")
}

func TestReloadKeyProviderReturnsError(t *testing.T) {
	publicKey, _ := newTestPublicKey(t)

	service, err := NewAdminService(log.New(), &provider.ProviderConfig{})
	require.NoError(t, err)

	mock := &mockKeysReloader{
		stubKeysProvider: stubKeysProvider{publicKey: publicKey},
		reloadKeyError:   fmt.Errorf("vault connection failed"),
	}
	service.SetKeysProvider(mock)

	err = service.ReloadKey(context.Background(), "test-key")
	require.Error(t, err)
	require.Contains(t, err.Error(), "ReloadKey Failed")
	require.Equal(t, 1, mock.reloadKeyCalls)
}
