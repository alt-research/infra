package provider

import (
	"crypto/ecdsa"
	"fmt"
	"strings"

	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/log"
	"github.com/hashicorp/vault/api"
)

// VaultOnePassSignatureProvider implements SignatureProvider using local private keys
type VaultOnePassSignatureProvider struct {
	*LocalKMSSignatureProvider

	vaultClient *api.Client
}

// NewVaultOnePassSignatureProvider creates a new VaultOnePassSignatureProvider and loads all configured keys
func NewVaultOnePassSignatureProvider(logger log.Logger, config ProviderConfig) (SignatureProvider, error) {
	provider := &VaultOnePassSignatureProvider{
		LocalKMSSignatureProvider: &LocalKMSSignatureProvider{
			logger: logger,
			config: config,
			keyMap: make(map[string]*ecdsa.PrivateKey),
		},
	}

	// Load all keys during construction
	for _, auth := range config.Auth {
		if err := provider.loadKey(auth.KeyName, auth.FieldName); err != nil {
			return nil, fmt.Errorf("failed to load key from path '%s': %w", auth.KeyName, err)
		}
	}

	return provider, nil
}

// parsePrivateKey parses a private key from a PEM-formatted file
func (l *VaultOnePassSignatureProvider) parsePrivateKey(keyPath string, fieldName string) (*ecdsa.PrivateKey, error) {
	l.logger.Debug("parsing private key", "keyPath", keyPath)
	// Read from Vault 1Password plugin
	secret, err := l.vaultClient.Logical().Read(keyPath)
	if err != nil {
		return nil, fmt.Errorf("vault read failed: %w", err)
	}

	if secret == nil || secret.Data == nil {
		return nil, fmt.Errorf("no data returned from vault")
	}

	// Extract the private key field
	privateKeyHex, ok := secret.Data[fieldName].(string)
	if !ok {
		return nil, fmt.Errorf("field %s not found or not a string", fieldName)
	}

	// Remove 0x prefix if present
	privateKeyHex = strings.TrimPrefix(privateKeyHex, "0x")

	// Parse private key
	privateKey, err := crypto.HexToECDSA(privateKeyHex)
	if err != nil {
		return nil, fmt.Errorf("invalid private key: %w", err)
	}

	return privateKey, nil
}

// loadKey loads a private key from a file path and stores it in the key map
func (l *VaultOnePassSignatureProvider) loadKey(keyPath string, fieldName string) error {
	l.logger.Debug("loading key from path", "keyPath", keyPath)
	key, err := l.parsePrivateKey(keyPath, fieldName)
	if err != nil {
		return fmt.Errorf("failed to load key from path '%s': %w", keyPath, err)
	}
	l.keyMap[keyPath] = key
	l.logger.Info("loaded private key",
		"keyPath", keyPath,
		"address", crypto.PubkeyToAddress(key.PublicKey).Hex())
	return nil
}
