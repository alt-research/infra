package admin

import (
	"context"
	"testing"

	"github.com/ethereum-optimism/infra/op-signer/provider"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/log"
	"github.com/stretchr/testify/require"
)

func TestGetConfigsReturnsAddressAndMetadata(t *testing.T) {
	key, err := crypto.GenerateKey()
	require.NoError(t, err)
	addr := crypto.PubkeyToAddress(key.PublicKey)

	cfg := &provider.ProviderConfig{}
	cfg.AddConfig(addr.Hex(), provider.AuthConfig{
		KeyName:         "projects/my-project/locations/us-west1/keyRings/my-ring/cryptoKeys/my-key/cryptoKeyVersions/1",
		ChainID:         11155111,
		FromAddress:     common.HexToAddress(addr.Hex()),
		AllowedClientCN: "myshell-testnet-challenger-0",
	})

	svc, err := NewAdminService(log.New(), cfg)
	require.NoError(t, err)

	configs, err := svc.GetConfigs(context.Background())
	require.NoError(t, err)
	require.Len(t, configs, 1)

	got := configs[0]
	require.Equal(t, addr.Hex(), got.Address)
	require.Equal(t, "projects/my-project/locations/us-west1/keyRings/my-ring/cryptoKeys/my-key/cryptoKeyVersions/1", got.Path)
	require.Equal(t, uint64(11155111), got.ParentChainID)
	require.Equal(t, "myshell-testnet-challenger-0", got.AllowedClientCN)
}

func TestGetConfigsEmpty(t *testing.T) {
	svc, err := NewAdminService(log.New(), &provider.ProviderConfig{})
	require.NoError(t, err)

	configs, err := svc.GetConfigs(context.Background())
	require.NoError(t, err)
	require.Empty(t, configs)
}
