package provider

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/log"
)

func TestReadConfigFromJSON(t *testing.T) {
	tempFile := "/tmp/test_provider_config.json"
	defer os.Remove(tempFile)

	logger := log.NewLogger(log.DiscardHandler())

	// Write a valid plaintext config
	cfg := ProviderConfigJSON{
		ProviderType: KeyProviderGCP,
		Auth: []AuthConfig{
			{
				ClientName:      "test_client",
				KeyName:         "test_key",
				ChainID:         1,
				FromAddress:     common.HexToAddress("0x1234567890123456789012345678901234567890"),
				ToAddresses:     []string{"0x1234567890123456789012345678901234567890"},
				MaxValue:        "0x0",
				AllowedClientCN: "test_cn",
			},
		},
	}
	data, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("Failed to marshal config: %v", err)
	}
	if err := os.WriteFile(tempFile, data, 0600); err != nil {
		t.Fatalf("Failed to write temp file: %v", err)
	}

	loadedConfig, err := ReadConfigFromJSON(logger, tempFile)
	if err != nil {
		t.Fatalf("Failed to read config from JSON: %v", err)
	}

	if loadedConfig.Type() != KeyProviderGCP {
		t.Errorf("Expected provider type GCP, got %s", loadedConfig.Type())
	}

	authConfigs := loadedConfig.Auth()
	if len(authConfigs) != 1 {
		t.Fatalf("Expected 1 auth config, got %d", len(authConfigs))
	}
	if authConfigs[0].ClientName != "test_client" {
		t.Errorf("Expected client name 'test_client', got %s", authConfigs[0].ClientName)
	}
	if authConfigs[0].KeyName != "test_key" {
		t.Errorf("Expected key name 'test_key', got %s", authConfigs[0].KeyName)
	}
}

func TestReadConfigFromJSON_InMemoryMutations(t *testing.T) {
	tempFile := "/tmp/test_provider_config_mutations.json"
	defer os.Remove(tempFile)

	logger := log.NewLogger(log.DiscardHandler())

	cfg := ProviderConfigJSON{
		ProviderType: KeyProviderGCP,
		Auth: []AuthConfig{
			{
				ClientName:  "original_client",
				KeyName:     "original_key",
				FromAddress: common.HexToAddress("0x1234567890123456789012345678901234567890"),
				MaxValue:    "0x0",
			},
		},
	}
	data, _ := json.Marshal(cfg)
	os.WriteFile(tempFile, data, 0600)

	loadedConfig, err := ReadConfigFromJSON(logger, tempFile)
	if err != nil {
		t.Fatalf("Failed to read config: %v", err)
	}

	// AddConfig is in-memory only — file should not change
	loadedConfig.AddConfig("0xdeadbeef", AuthConfig{
		ClientName:  "new_client",
		KeyName:     "new_key",
		FromAddress: common.HexToAddress("0xdeadbeefdeadbeefdeadbeefdeadbeefdeadbeef"),
		MaxValue:    "0x0",
	})

	if len(loadedConfig.Auth()) != 2 {
		t.Errorf("Expected 2 auth configs in memory, got %d", len(loadedConfig.Auth()))
	}

	// File on disk should still have only 1 entry
	reloaded, err := ReadConfigFromJSON(logger, tempFile)
	if err != nil {
		t.Fatalf("Failed to re-read config: %v", err)
	}
	if len(reloaded.Auth()) != 1 {
		t.Errorf("Expected file to still have 1 auth config, got %d", len(reloaded.Auth()))
	}

	t.Log("In-memory mutation test passed!")
}
