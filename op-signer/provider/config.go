package provider

import (
	"errors"
	"fmt"
	"math/big"
	"os"
	"sync"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"gopkg.in/yaml.v3"
)

type AuthConfig struct {
	// ClientName DNS name of the client connecting to op-signer.
	ClientName string `yaml:"name"`
	// KeyName key locator for the KMS (resource name in cloud provider, or path to private key file for local provider)
	KeyName string `yaml:"key"`
	// ChainID chain id of the op-signer to sign for
	ChainID uint64 `yaml:"chainID"`
	// FromAddress sender address that is sending the rpc request
	FromAddress     common.Address `yaml:"fromAddress"`
	ToAddresses     []string       `yaml:"toAddresses"`
	MaxValue        string         `yaml:"maxValue"`
	AllowedClientCN string         `yaml:"allowed_client_cn"` // Optional Common Name restriction for client TLS certs
}

func (c AuthConfig) MaxValueToInt() *big.Int {
	return hexutil.MustDecodeBig(c.MaxValue)
}

type ProviderConfig struct {
	providerType ProviderType `yaml:"provider"`
	auth         []AuthConfig `yaml:"auth"`

	mu sync.RWMutex
}

func (c *ProviderConfig) AddConfig(address string, authConfig AuthConfig) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.auth = append(c.auth, authConfig)
}

func (c *ProviderConfig) RemoveConfig(address string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	nexAuthCfg := make([]AuthConfig, 0, len(c.auth))

	for _, ac := range c.auth {
		if ac.FromAddress.Hex() != address {
			nexAuthCfg = append(nexAuthCfg, ac)
		}
	}

	c.auth = nexAuthCfg

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

func ReadConfig(path string) (*ProviderConfig, error) {
	config := ProviderConfig{}
	data, err := os.ReadFile(path)
	if err != nil {
		return &config, err
	}
	if err := yaml.Unmarshal(data, &config); err != nil {
		return &config, err
	}

	// Default to GCP if Provider is empty
	if config.providerType == "" {
		config.providerType = KeyProviderGCP
	}

	if !config.providerType.IsValid() {
		return &config, fmt.Errorf("invalid provider '%s' in config. Must be 'AWS', 'GCP', 'LOCAL', or 'LOCALKEY'", config.providerType)
	}

	for _, authConfig := range config.auth {
		for _, toAddress := range authConfig.ToAddresses {
			if _, err := hexutil.Decode(toAddress); err != nil {
				return &config, fmt.Errorf("invalid toAddress '%s' in auth config: %w", toAddress, err)
			}
			if authConfig.MaxValue != "" {
				if _, err := hexutil.DecodeBig(authConfig.MaxValue); err != nil {
					return &config, fmt.Errorf("invalid maxValue '%s' in auth config: %w", toAddress, err)
				}
			}
		}
	}
	return &config, err
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
