package provider

import (
	"os"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/log"
)

func TestJSONPersistence(t *testing.T) {
	// Create a temporary JSON file for testing
	tempFile := "/tmp/test_provider_config.json"
	defer os.Remove(tempFile) // Clean up after test

	// Create a new ProviderConfig
	config := &ProviderConfig{
		providerType:        KeyProviderVault1Pass,
		encryptionKey:       DeriveEncryptionKey("123456"),
		persistenceFilePath: tempFile,
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

	// Check that the file was created and contains the data
	data, err := os.ReadFile(tempFile)
	if err != nil {
		t.Fatalf("Failed to read temp file: %v", err)
	}

	if len(data) == 0 {
		t.Fatal("Config file is empty")
	}

	logger := log.NewLogger(log.DiscardHandler())

	// Create a new config instance and load from the JSON file
	loadedConfig, err := ReadConfigFromJSON(logger, tempFile, DeriveEncryptionKey("123456"))
	if err != nil {
		t.Fatalf("Failed to read config from JSON: %v", err)
	}

	if loadedConfig.Type() != KeyProviderVault1Pass {
		t.Errorf("Expected provider type GCP, got %s", loadedConfig.Type())
	}

	authConfigs := loadedConfig.Auth()
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

	// Reload and verify it's gone
	loadedConfig2, err := ReadConfigFromJSON(logger, tempFile, DeriveEncryptionKey("123456"))
	if err != nil {
		t.Fatalf("Failed to read config from JSON after removal: %v", err)
	}
	authConfigs2 := loadedConfig2.Auth()
	if len(authConfigs2) != 0 {
		t.Errorf("Expected 0 auth configs after removal, got %d", len(authConfigs2))
	}

	t.Log("JSON persistence test passed!")
}
