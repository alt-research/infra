package provider

import (
	"testing"

	"github.com/ethereum/go-ethereum/common"
)

func TestBasicConfigOperations(t *testing.T) {
	// Create a new ProviderConfig
	config := &ProviderConfig{
		providerType: KeyProviderGCP,
	}

	// Add a test auth config
	testAuthConfig := AuthConfig{
		ClientName:      "test_client",
		KeyName:         "test_key",
		ChainID:         1,
		FromAddress:     common.HexToAddress("0x1234567890123456789012345678901234567890"),
		ToAddresses:     []string{"0x1234567890123456789012345678901234567890"},
		MaxValue:        "0x0",
		AllowedClientCN: "test_cn",
	}

	// Add the config
	config.AddConfig("0x1234567890123456789012345678901234567890", testAuthConfig)

	authConfigs := config.Auth()
	if len(authConfigs) != 1 {
		t.Errorf("Expected 1 auth config, got %d", len(authConfigs))
	} else {
		if authConfigs[0].ClientName != "test_client" {
			t.Errorf("Expected client name 'test_client', got %s", authConfigs[0].ClientName)
		}
		if authConfigs[0].KeyName != "test_key" {
			t.Errorf("Expected key name 'test_key', got %s", authConfigs[0].KeyName)
		}
	}

	// Test removing a config
	config.RemoveConfig("0x1234567890123456789012345678901234567890")

	authConfigs2 := config.Auth()
	if len(authConfigs2) != 0 {
		t.Errorf("Expected 0 auth configs after removal, got %d", len(authConfigs2))
	}

	t.Log("Basic config operations test passed!")
}