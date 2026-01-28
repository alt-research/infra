package provider

import (
	"crypto/ecdsa"
	"fmt"
	"os"

	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/log"
)

// LocalPrivateKeySignatureProvider implements SignatureProvider using local private keys
type LocalPrivateKeySignatureProvider struct {
	*LocalKMSSignatureProvider
}

// NewLocalPrivateKeySignatureProvider creates a new LocalPrivateKeySignatureProvider and loads all configured keys
func NewLocalPrivateKeySignatureProvider(logger log.Logger, config ProviderConfig) (SignatureProvider, error) {
	provider := &LocalPrivateKeySignatureProvider{
		LocalKMSSignatureProvider: &LocalKMSSignatureProvider{
			logger: logger,
			config: config,
			keyMap: make(map[string]*ecdsa.PrivateKey),
		},
	}

	// Load all keys during construction
	for _, auth := range config.Auth {
		if err := provider.loadKey(auth.KeyName); err != nil {
			return nil, fmt.Errorf("failed to load key from path '%s': %w", auth.KeyName, err)
		}
	}

	return provider, nil
}

// parsePrivateKey parses a private key from a PEM-formatted file
func (l *LocalPrivateKeySignatureProvider) parsePrivateKey(keyPath string) (*ecdsa.PrivateKey, error) {
	l.logger.Debug("parsing private key", "keyPath", keyPath)

	key, err := l.tryParsePrivateKeyHex(keyPath)
	if err != nil {
		l.logger.Debug("failed to parse private key as hex, use KMS signature", "keyPath", keyPath, "err", err)
		return l.LocalKMSSignatureProvider.parsePrivateKey(keyPath)
	}

	// Verify it's using secp256k1 curve
	if key.Curve != crypto.S256() {
		return nil, fmt.Errorf("key from path '%s' must use secp256k1 curve (got %s)", keyPath, key.Curve.Params().Name)
	}

	l.logger.Debug("successfully parsed private key", "keyPath", keyPath, "curve", key.Curve.Params().Name)
	return key, nil
}

// parsePrivateKey parses a private key from a PEM-formatted file
func (l *LocalPrivateKeySignatureProvider) tryParsePrivateKeyHex(keyPath string) (*ecdsa.PrivateKey, error) {
	l.logger.Debug("parsing private key", "keyPath", keyPath)
	// Read the private key file
	keyBytes, err := os.ReadFile(keyPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read private key from path '%s': %w", keyPath, err)
	}

	key, err := crypto.HexToECDSA(string(keyBytes))
	if err != nil {
		return nil, fmt.Errorf("failed to parse private key hex from path '%s': %w", keyPath, err)
	}

	return key, nil

}

// loadKey loads a private key from a file path and stores it in the key map
func (l *LocalPrivateKeySignatureProvider) loadKey(keyPath string) error {
	l.logger.Debug("loading key from path", "keyPath", keyPath)
	key, err := l.parsePrivateKey(keyPath)
	if err != nil {
		return fmt.Errorf("failed to load key from path '%s': %w", keyPath, err)
	}
	l.keyMap[keyPath] = key
	l.logger.Info("loaded private key",
		"keyPath", keyPath,
		"address", crypto.PubkeyToAddress(key.PublicKey).Hex())
	return nil
}
