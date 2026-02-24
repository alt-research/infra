package provider

import (
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"os"
	"strings"
	"sync"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/log"
)

type AuthConfig struct {
	// ClientName DNS name of the client connecting to op-signer.
	ClientName string `yaml:"name" json:"name"`
	// KeyName key locator for the KMS (resource name in cloud provider, or path to private key file for local provider)
	KeyName string `yaml:"key" json:"key"`
	// ChainID chain id of the op-signer to sign for
	ChainID uint64 `yaml:"chainID" json:"chainID"`
	// FromAddress sender address that is sending the rpc request
	FromAddress     common.Address `yaml:"fromAddress" json:"fromAddress"`
	ToAddresses     []string       `yaml:"toAddresses" json:"toAddresses"`
	MaxValue        string         `yaml:"maxValue" json:"maxValue"`
	AllowedClientCN string         `yaml:"allowed_client_cn" json:"allowed_client_cn"` // Optional Common Name restriction for client TLS certs
}

func (c AuthConfig) MaxValueToInt() *big.Int {
	return hexutil.MustDecodeBig(c.MaxValue)
}

type ProviderConfig struct {
	providerType ProviderType `yaml:"provider" json:"provider"`
	auth         []AuthConfig `yaml:"auth" json:"auth"`
	pathPrefix   string       `yaml:"path_prefix" json:"path_prefix"`

	persistenceFilePath string
	encryptionKey       []byte
	mu                  sync.RWMutex
}

type ProviderConfigJSON struct {
	ProviderType ProviderType `json:"provider"`
	Auth         []AuthConfig `json:"auth"`
}

func TryExtractPath(url string, pathRootPrefix string) string {
	subPath := strings.Split(url, "/")

	res := make([]string, 0, len(subPath)+2)

	if pathRootPrefix != "" {
		pathRoots := strings.Split(pathRootPrefix, "/")
		pathRoots = append(pathRoots, subPath...)
		subPath = pathRoots
	}

	for i := 0; i < len(subPath); i++ {
		if subPath[i] != "" {
			res = append(res, subPath[i])
		}
	}

	return strings.Join(res, "/")
}

func MakeFullPath(pathRootPrefix string, url string) string {
	return TryExtractPath(url, pathRootPrefix)
}

func (c *ProviderConfig) SetPathPrefix(prefix string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.pathPrefix = prefix
}

func (c *ProviderConfig) PathPrefix() string {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.pathPrefix
}

func (c *ProviderConfig) saveToJSON() error {
	if c.persistenceFilePath == "" {
		return nil // No persistence file set, skip saving
	}

	// Prepare a copy of the config for marshaling to avoid holding the lock during file I/O
	configCopy := ProviderConfigJSON{
		ProviderType: c.providerType,
		Auth:         make([]AuthConfig, len(c.auth)),
	}
	copy(configCopy.Auth, c.auth)

	data, err := json.MarshalIndent(configCopy, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal config to JSON: %w", err)
	}

	encrypted, err := Encrypt(data, c.encryptionKey)
	if err != nil {
		return fmt.Errorf("failed to encrypt config JSON: %w", err)
	}

	// Write to a temporary file first, then rename to prevent corruption during write
	tempFile := c.persistenceFilePath + ".tmp"
	if err := os.WriteFile(tempFile, encrypted, 0644); err != nil {
		return fmt.Errorf("failed to write config to temp file: %w", err)
	}

	if err := os.Rename(tempFile, c.persistenceFilePath); err != nil {
		return fmt.Errorf("failed to rename temp file: %w", err)
	}

	return nil
}

func (c *ProviderConfig) AddConfig(address string, authConfig AuthConfig) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.auth = append(c.auth, authConfig)

	// Attempt to persist to JSON file
	if c.persistenceFilePath != "" {
		// Save to JSON while holding the lock
		_ = c.saveToJSON()
	}
}

func (c *ProviderConfig) RemoveConfig(address string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	newAuthCfg := make([]AuthConfig, 0, len(c.auth))

	for _, ac := range c.auth {
		if ac.FromAddress.Hex() != address {
			newAuthCfg = append(newAuthCfg, ac)
		}
	}

	c.auth = newAuthCfg

	// Attempt to persist to JSON file
	if c.persistenceFilePath != "" {
		_ = c.saveToJSON()
	}
}

func (c *ProviderConfig) RemoveConfigByPath(path string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	newAuthCfg := make([]AuthConfig, 0, len(c.auth))
	for _, ac := range c.auth {
		if ac.KeyName != path {
			newAuthCfg = append(newAuthCfg, ac)
		}
	}
	c.auth = newAuthCfg

	// Attempt to persist to JSON file
	if c.persistenceFilePath != "" {
		_ = c.saveToJSON()
	}
}

func (c *ProviderConfig) GetConfigByPath(path string) (*AuthConfig, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	for _, ac := range c.auth {
		if ac.KeyName == path {
			return &ac, nil
		}
	}
	return nil, fmt.Errorf("no auth config found for path '%s'", path)
}

func (c *ProviderConfig) GetConfigByAddress(address string) (*AuthConfig, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	for _, authConfig := range c.auth {
		if authConfig.FromAddress.Hex() == address {
			return &authConfig, nil
		}
	}

	return nil, fmt.Errorf("no config found for address %s", address)
}

func (c ProviderConfig) Type() ProviderType {
	return c.providerType
}

func (c *ProviderConfig) Auth() []AuthConfig {
	c.mu.RLock()
	defer c.mu.RUnlock()

	res := make([]AuthConfig, len(c.auth))
	copy(res, c.auth)

	return res
}

func tryToReadConfigFromRawJSON(data []byte, encryptionKey []byte) *ProviderConfigJSON {

	// try to decrypt the json raw data
	// Create a temporary config to unmarshal to
	tempConfig := ProviderConfigJSON{}

	if err := json.Unmarshal(data, &tempConfig); err != nil {
		return nil
	}

	if tempConfig.ProviderType != "" && len(tempConfig.Auth) > 0 && tempConfig.Auth[0].KeyName != "" {
		return &tempConfig
	}

	return nil
}

func readConfigFromJSON(encrypted []byte, encryptionKey []byte) (*ProviderConfigJSON, error) {
	jsonConfig := tryToReadConfigFromRawJSON(encrypted, encryptionKey)
	if jsonConfig != nil {
		return jsonConfig, nil
	}

	// Decrypt data
	data, err := Decrypt(encrypted, encryptionKey)
	if err != nil {
		return nil, fmt.Errorf("failed to decrypt config: %w", err)
	}

	jsonConfigFromDecrypted := tryToReadConfigFromRawJSON(data, encryptionKey)
	if jsonConfigFromDecrypted != nil {
		return jsonConfigFromDecrypted, nil
	}

	// Empty config
	return &ProviderConfigJSON{}, nil
}

// ReadConfigFromJSON reads a ProviderConfig from a JSON file
func ReadConfigFromJSON(log log.Logger, path string, encryptionKey []byte) (*ProviderConfig, error) {
	// Check if file exists
	if _, err := os.Stat(path); os.IsNotExist(err) {
		log.Info("Config file does not exist, creating default config", "path", path)
		return &ProviderConfig{
			providerType:        KeyProviderVault1Pass,
			auth:                make([]AuthConfig, 0, 16),
			encryptionKey:       encryptionKey,
			persistenceFilePath: path,
		}, nil
	}

	encrypted, err := os.ReadFile(path)
	if err != nil {
		config := ProviderConfig{}
		log.Error("Failed to read config file", "path", path, "error", err)
		return &config, err
	}

	tempConfig, err := readConfigFromJSON(encrypted, encryptionKey)
	if err != nil {
		log.Error("Failed to read config from JSON", "path", path, "error", err)
		return nil, err
	}

	log.Debug("Successfully read config from JSON", "path", path)

	config := &ProviderConfig{
		providerType:        tempConfig.ProviderType,
		auth:                tempConfig.Auth,
		encryptionKey:       encryptionKey,
		persistenceFilePath: path,
	}

	// Default to GCP if Provider is empty
	if config.providerType == "" {
		config.providerType = KeyProviderVault1Pass
	}

	if !config.providerType.IsValid() {
		return config, fmt.Errorf("invalid provider '%s' in config. Must be 'AWS', 'GCP', 'LOCAL', or 'LOCALKEY'", config.providerType)
	}

	for _, authConfig := range config.auth {
		for _, toAddress := range authConfig.ToAddresses {
			if _, err := hexutil.Decode(toAddress); err != nil {
				return config, fmt.Errorf("invalid toAddress '%s' in auth config: %w", toAddress, err)
			}
			if authConfig.MaxValue != "" {
				if _, err := hexutil.DecodeBig(authConfig.MaxValue); err != nil {
					return config, fmt.Errorf("invalid maxValue '%s' in auth config: %w", toAddress, err)
				}
			}
		}
	}
	return config, err
}

func (s *ProviderConfig) GetAuthConfigForClient(clientName string, fromAddress *common.Address) (*AuthConfig, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if clientName == "" {
		return nil, errors.New("client name is empty")
	}
	for _, ac := range s.auth {
		if ac.ClientName == clientName {
			// If fromAddress is specified, it must match the address in the authConfig
			if fromAddress != nil && *fromAddress != ac.FromAddress {
				continue
			}

			return &ac, nil
		}
	}

	// allow empty client name to support more flexible usage
	return &AuthConfig{
		ClientName: clientName,
		KeyName:    clientName,
	}, nil
}
