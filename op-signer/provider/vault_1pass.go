package provider

import (
	"context"
	"crypto/ecdsa"
	"fmt"
	"strings"
	"sync"

	"github.com/ethereum-optimism/infra/op-signer/provider/vault"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/log"
	"github.com/hashicorp/vault/api"
)

// VaultOnePassSignatureProvider implements SignatureProvider using local private keys
type VaultOnePassSignatureProvider struct {
	logger log.Logger
	config ProviderConfig

	mu     sync.RWMutex
	keyMap map[string]*ecdsa.PrivateKey

	vaultClient *api.Client
	vaultCfg    vault.VaultAuthConfig
}

// NewVaultOnePassSignatureProvider creates a new VaultOnePassSignatureProvider and loads all configured keys
func NewVaultOnePassSignatureProvider(logger log.Logger, config ProviderConfig) (SignatureProvider, error) {
	// Load Vault authentication configuration
	authCfg := vault.LoadVaultAuthConfig(logger)

	// Initialize and authenticate Vault client
	client, err := vault.NewVaultClient(logger, authCfg)
	if err != nil {
		logger.Error("Failed to initialize Vault client", "error", err)

		return nil, fmt.Errorf("failed to initialize Vault client: %w", err)
	}

	provider := &VaultOnePassSignatureProvider{
		logger:      logger,
		config:      config,
		keyMap:      make(map[string]*ecdsa.PrivateKey, 128),
		vaultClient: client,
		vaultCfg:    authCfg,
	}

	// Load all keys during construction
	for _, auth := range config.Auth {
		if err := provider.tryLoadKey(auth.KeyName); err != nil {
			return nil, fmt.Errorf("failed to load key '%s': %w", auth.KeyName, err)
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

	l.logger.Debug("secrets", "secret", secret.Data)

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
	l.mu.Lock()
	defer l.mu.Unlock()

	l.logger.Debug("loading key from path", "keyPath", keyPath, "fieldName", fieldName)
	key, err := l.parsePrivateKey(keyPath, fieldName)
	if err != nil {
		return fmt.Errorf("failed to load key from path '%s': %w", keyPath, err)
	}

	l.keyMap[KeyName(keyPath, fieldName)] = key
	l.logger.Info("loaded private key",
		"keyPath", keyPath,
		"address", crypto.PubkeyToAddress(key.PublicKey).Hex())
	return nil
}

func KeyName(vaultPath string, fieldName string) string {
	return fmt.Sprintf("%s/%s", vaultPath, fieldName)
}

func VaultPathAndFieldName(keyName string) (string, string, error) {
	path := strings.Split(keyName, "/")

	if len(path) < 2 {
		return "", "", fmt.Errorf("invalid key name '%s', must be in format 'path/fieldName'", keyName)
	}

	fieldName := path[len(path)-1]
	vaultPath := strings.Join(path[:len(path)-1], "/")

	return vaultPath, fieldName, nil
}

func FieldName(keyName string) (string, error) {
	path := strings.Split(keyName, "/")
	if len(path) < 2 {
		return "", fmt.Errorf("invalid key name '%s', must be in format 'vault/path/fieldName'", keyName)
	}

	return path[len(path)-1], nil

}

func (l *VaultOnePassSignatureProvider) isAllowedPath(path string) bool {
	for _, prefix := range l.vaultCfg.AllowPathPrefixs {
		if strings.HasPrefix(path, prefix) {
			return true
		}
	}

	return false
}

func (l *VaultOnePassSignatureProvider) tryLoadKey(keyName string) error {
	_, ok := l.getPrivateKey(keyName)
	if ok {
		return nil
	}

	if !l.isAllowedPath(keyName) {
		l.logger.Error("Vault path is not allowed", "keyName", keyName)
		return fmt.Errorf("vault path is not allowed: %s", keyName)
	}

	vaultPath, fieldName, err := VaultPathAndFieldName(keyName)
	if err != nil {
		l.logger.Error("Failed to parse vault path and field name", "keyName", keyName, "error", err)
		return fmt.Errorf("failed to parse vault path and field name: %w", err)
	}

	l.logger.Info("Loading key from vault", "keyName", keyName, "vaultPath", vaultPath, "fieldName", fieldName)

	if err := l.loadKey(vaultPath, fieldName); err != nil {
		l.logger.Error("Failed to load key from vault", "keyName", keyName, "error", err)
		return fmt.Errorf("failed to load key from vault: %w", err)
	}

	return nil
}

func (l *VaultOnePassSignatureProvider) getPrivateKey(keyName string) (*ecdsa.PrivateKey, bool) {
	l.mu.RLock()
	defer l.mu.RUnlock()

	res, ok := l.keyMap[keyName]
	return res, ok
}

// SignDigest signs the digest using the local private key and returns a compact recoverable signature
func (l *VaultOnePassSignatureProvider) SignDigest(
	ctx context.Context,
	keyName string,
	digest []byte,
) ([]byte, error) {
	l.logger.Debug("signing digest", "keyName", keyName, "digestLength", len(digest))
	if err := l.tryLoadKey(keyName); err != nil {
		return nil, fmt.Errorf("failed to load key '%s': %w", keyName, err)
	}

	privateKey, ok := l.getPrivateKey(keyName)
	if !ok {
		return nil, fmt.Errorf("key '%s' not found in key map", keyName)
	}

	signature, err := crypto.Sign(digest, privateKey)
	if err != nil {
		return nil, fmt.Errorf("failed to sign digest")
	}

	l.logger.Debug("successfully signed digest", "keyName", keyName, "signatureLength", len(signature))
	return signature, nil
}

// GetPublicKey returns the public key in uncompressed format
func (l *VaultOnePassSignatureProvider) GetPublicKey(
	ctx context.Context,
	keyName string,
) ([]byte, error) {
	l.logger.Debug("retrieving public key", "keyName", keyName)
	if err := l.tryLoadKey(keyName); err != nil {
		return nil, fmt.Errorf("failed to load key '%s': %w", keyName, err)
	}

	privateKey, ok := l.getPrivateKey(keyName)
	if !ok {
		return nil, fmt.Errorf("key '%s' not found in key map", keyName)
	}

	pubKey := crypto.FromECDSAPub(&privateKey.PublicKey)
	l.logger.Debug("retrieved public key", "keyName", keyName, "pubKeyLength", len(pubKey))
	return pubKey, nil
}
