package provider

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

// MockKeyReloader is a mock implementation of KeyReloader for testing
type MockKeyReloader struct {
	reloadKeyCalled bool
	reloadKeyError  error
	reloadKeyName   string
}

func (m *MockKeyReloader) ReloadKey(_ context.Context, keyName string) error {
	m.reloadKeyCalled = true
	m.reloadKeyName = keyName
	return m.reloadKeyError
}

func TestKeyReloaderInterface(t *testing.T) {
	// Verify VaultOnePassSignatureProvider implements KeyReloader interface
	var _ KeyReloader = (*VaultOnePassSignatureProvider)(nil)
}

func TestMockKeyReloader(t *testing.T) {
	mock := &MockKeyReloader{}

	err := mock.ReloadKey(context.Background(), "test/key")
	require.NoError(t, err)
	require.True(t, mock.reloadKeyCalled)
	require.Equal(t, "test/key", mock.reloadKeyName)
}

func TestMockKeyReloaderWithError(t *testing.T) {
	mock := &MockKeyReloader{
		reloadKeyError: context.DeadlineExceeded,
	}

	err := mock.ReloadKey(context.Background(), "test/key")
	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.True(t, mock.reloadKeyCalled)
}
