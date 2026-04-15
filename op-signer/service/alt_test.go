package service

import (
	"context"
	"fmt"
	"testing"

	"github.com/ethereum-optimism/infra/op-signer/provider"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/log"
	"github.com/stretchr/testify/require"
)

// mockKeyReloaderProvider is a mock provider that implements both SignatureProvider and KeyReloader
type mockKeyReloaderProvider struct {
	publicKey      []byte
	reloadKeyCalls int
	reloadKeyError error
}

func (m *mockKeyReloaderProvider) GetPublicKey(_ context.Context, _ string) ([]byte, error) {
	return m.publicKey, nil
}

func (m *mockKeyReloaderProvider) SignDigest(_ context.Context, _ string, _ []byte) ([]byte, error) {
	return nil, nil
}

func (m *mockKeyReloaderProvider) ReloadKey(_ context.Context, _ string) error {
	m.reloadKeyCalls++
	return m.reloadKeyError
}

// mockNonReloaderProvider is a mock provider that does NOT implement KeyReloader
type mockNonReloaderProvider struct {
	publicKey []byte
}

func (m *mockNonReloaderProvider) GetPublicKey(_ context.Context, _ string) ([]byte, error) {
	return m.publicKey, nil
}

func (m *mockNonReloaderProvider) SignDigest(_ context.Context, _ string, _ []byte) ([]byte, error) {
	return nil, nil
}

func TestAltServiceReloadKeySuccess(t *testing.T) {
	key, err := crypto.GenerateKey()
	require.NoError(t, err)
	publicKey := crypto.FromECDSAPub(&key.PublicKey)
	keyName := "test/vault/path/key"

	// Create provider config with the key
	config := &provider.ProviderConfig{}
	config.AddConfig(crypto.PubkeyToAddress(key.PublicKey).Hex(), provider.AuthConfig{
		ClientName:  "test-client",
		KeyName:     keyName,
		FromAddress: crypto.PubkeyToAddress(key.PublicKey),
	})

	// Create mock provider that supports reloading
	mockProvider := &mockKeyReloaderProvider{
		publicKey: publicKey,
	}

	altService := &AltService{
		logger:   log.New(),
		config:   config,
		provider: mockProvider,
	}

	// Create context with client info
	ctx := context.WithValue(context.Background(), clientInfoContextKey{}, ClientInfo{
		ClientName: "test-client",
	})

	err = altService.ReloadKey(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, mockProvider.reloadKeyCalls)
}

func TestAltServiceReloadKeyProviderNotSupportReloader(t *testing.T) {
	key, err := crypto.GenerateKey()
	require.NoError(t, err)
	publicKey := crypto.FromECDSAPub(&key.PublicKey)
	keyName := "test/vault/path/key"

	// Create provider config with the key
	config := &provider.ProviderConfig{}
	config.AddConfig(crypto.PubkeyToAddress(key.PublicKey).Hex(), provider.AuthConfig{
		ClientName:  "test-client",
		KeyName:     keyName,
		FromAddress: crypto.PubkeyToAddress(key.PublicKey),
	})

	// Create mock provider that does NOT support reloading
	mockProvider := &mockNonReloaderProvider{
		publicKey: publicKey,
	}

	altService := &AltService{
		logger:   log.New(),
		config:   config,
		provider: mockProvider,
	}

	// Create context with client info
	ctx := context.WithValue(context.Background(), clientInfoContextKey{}, ClientInfo{
		ClientName: "test-client",
	})

	err = altService.ReloadKey(ctx)
	require.Error(t, err)
	require.Contains(t, err.Error(), "provider does not support key reloading")
}

func TestAltServiceReloadKeyEmptyClientName(t *testing.T) {
	key, err := crypto.GenerateKey()
	require.NoError(t, err)
	publicKey := crypto.FromECDSAPub(&key.PublicKey)

	// Create provider config with the key
	config := &provider.ProviderConfig{}

	mockProvider := &mockKeyReloaderProvider{
		publicKey: publicKey,
	}

	altService := &AltService{
		logger:   log.New(),
		config:   config,
		provider: mockProvider,
	}

	// Create context with empty client name (will return Forbidden)
	ctx := context.WithValue(context.Background(), clientInfoContextKey{}, ClientInfo{
		ClientName: "",
	})

	err = altService.ReloadKey(ctx)
	require.Error(t, err)
	require.Contains(t, err.Error(), "Forbidden")
	require.Equal(t, 0, mockProvider.reloadKeyCalls) // Should not call ReloadKey
}

func TestAltServiceReloadKeyProviderReturnsError(t *testing.T) {
	key, err := crypto.GenerateKey()
	require.NoError(t, err)
	publicKey := crypto.FromECDSAPub(&key.PublicKey)
	keyName := "test/vault/path/key"

	// Create provider config with the key
	config := &provider.ProviderConfig{}
	config.AddConfig(crypto.PubkeyToAddress(key.PublicKey).Hex(), provider.AuthConfig{
		ClientName:  "test-client",
		KeyName:     keyName,
		FromAddress: crypto.PubkeyToAddress(key.PublicKey),
	})

	// Create mock provider that returns error on reload
	mockProvider := &mockKeyReloaderProvider{
		publicKey:      publicKey,
		reloadKeyError: fmt.Errorf("vault read failed"),
	}

	altService := &AltService{
		logger:   log.New(),
		config:   config,
		provider: mockProvider,
	}

	// Create context with client info
	ctx := context.WithValue(context.Background(), clientInfoContextKey{}, ClientInfo{
		ClientName: "test-client",
	})

	err = altService.ReloadKey(ctx)
	require.Error(t, err)
	require.Contains(t, err.Error(), "ReloadKey Failed")
	require.Equal(t, 1, mockProvider.reloadKeyCalls)
}

func TestKeyReloaderInterfaceAssertion(t *testing.T) {
	// Verify that mockKeyReloaderProvider implements both interfaces
	var _ provider.SignatureProvider = (*mockKeyReloaderProvider)(nil)
	var _ provider.KeyReloader = (*mockKeyReloaderProvider)(nil)

	// Verify that mockNonReloaderProvider only implements SignatureProvider
	var _ provider.SignatureProvider = (*mockNonReloaderProvider)(nil)
}
