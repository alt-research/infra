package vault

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/log"

	"github.com/hashicorp/vault/api"
)

const (
	// Default paths for Kubernetes service account
	defaultServiceAccountTokenPath = "/var/run/secrets/kubernetes.io/serviceaccount/token"
	defaultKubernetesAuthPath      = "auth/kubernetes/login"
)

// VaultAuthConfig holds configuration for Vault authentication
type VaultAuthConfig struct {
	// VaultAddr is the Vault server address
	VaultAddr string

	// AuthMethod is the authentication method to use ("kubernetes" or "token")
	AuthMethod string

	// Token is the static Vault token (used when AuthMethod="token")
	Token string

	// KubernetesRole is the Vault role to use for Kubernetes auth
	KubernetesRole string

	// ServiceAccountTokenPath is the path to the Kubernetes service account JWT
	ServiceAccountTokenPath string

	// TokenRenewInterval is how often to renew the Vault token
	TokenRenewInterval time.Duration

	// AllowPathPrefixs is whether to allow path prefixes
	AllowPathPrefixs []string
}

// NewVaultClient creates and authenticates a Vault client based on the configuration
func NewVaultClient(logger log.Logger, cfg VaultAuthConfig) (*api.Client, error) {
	// Create Vault client
	config := api.DefaultConfig()
	config.Address = cfg.VaultAddr
	if config.Address == "" {
		config.Address = "http://vault:8200"
	}

	client, err := api.NewClient(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create Vault client: %w", err)
	}

	// Authenticate based on method
	switch cfg.AuthMethod {
	case "kubernetes":
		if err := authenticateKubernetes(logger, client, cfg); err != nil {
			return nil, fmt.Errorf("kubernetes auth failed: %w", err)
		}
		// Start token renewal goroutine
		go renewToken(logger, client, cfg, cfg.TokenRenewInterval)

	case "token", "":
		// Traditional static token authentication
		if cfg.Token == "" {
			return nil, fmt.Errorf("VAULT_TOKEN required when using token auth")
		}
		client.SetToken(cfg.Token)

	default:
		return nil, fmt.Errorf("unsupported auth method: %s (use 'kubernetes' or 'token')", cfg.AuthMethod)
	}

	return client, nil
}

// authenticateKubernetes performs Kubernetes authentication with Vault
func authenticateKubernetes(logger log.Logger, client *api.Client, cfg VaultAuthConfig) error {
	// Read service account JWT
	jwtPath := cfg.ServiceAccountTokenPath
	if jwtPath == "" {
		jwtPath = defaultServiceAccountTokenPath
	}

	jwt, err := os.ReadFile(jwtPath)
	if err != nil {
		return fmt.Errorf("failed to read service account token from %s: %w", jwtPath, err)
	}

	// Authenticate with Vault

	logger.Info("Authenticating to Vault using Kubernetes auth", "role", cfg.KubernetesRole)

	secret, err := client.Logical().Write(defaultKubernetesAuthPath, map[string]interface{}{
		"role": cfg.KubernetesRole,
		"jwt":  string(jwt),
	})
	if err != nil {
		return fmt.Errorf("kubernetes auth login failed: %w", err)
	}

	if secret == nil || secret.Auth == nil {
		return fmt.Errorf("kubernetes auth login returned no token")
	}

	// Set the token
	client.SetToken(secret.Auth.ClientToken)

	logger.Info("Vault authentication successful",
		"token TTL", secret.Auth.LeaseDuration, "renewable", secret.Auth.Renewable)

	return nil
}

// renewToken periodically renews the Vault token, re-authenticating from scratch if renewal fails
func renewToken(logger log.Logger, client *api.Client, cfg VaultAuthConfig, interval time.Duration) {
	if interval == 0 {
		interval = 1 * time.Hour // Default renewal interval
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for range ticker.C {
		logger.Debug("Attempting to renew Vault token")

		secret, err := client.Auth().Token().RenewSelf(0)
		if err != nil {
			logger.Error("Failed to renew Vault token", "error", err)
			logger.Info("Re-authenticating to Vault via Kubernetes auth")
			if authErr := authenticateKubernetes(logger, client, cfg); authErr != nil {
				logger.Error("Re-authentication failed, will retry", "error", authErr, "interval", interval)
			} else {
				logger.Info("Re-authentication successful")
			}
			continue
		}

		if secret != nil && secret.Auth != nil {
			logger.Info("Vault token renewed successfully", "new TTL", secret.Auth.LeaseDuration)
		}
	}
}

// LoadVaultAuthConfig loads Vault authentication configuration from environment variables
func LoadVaultAuthConfig(logger log.Logger) VaultAuthConfig {
	allows := os.Getenv("OP_SIGNER_VAULT_ALLOW_PATH_PREFIXES")
	allowsArrays := strings.Split(allows, ",")

	cfg := VaultAuthConfig{
		VaultAddr:               os.Getenv("OP_SIGNER_VAULT_ADDR"),
		AuthMethod:              os.Getenv("OP_SIGNER_VAULT_AUTH_METHOD"),
		Token:                   os.Getenv("OP_SIGNER_VAULT_TOKEN"),
		KubernetesRole:          os.Getenv("OP_SIGNER_VAULT_KUBERNETES_ROLE"),
		ServiceAccountTokenPath: os.Getenv("OP_SIGNER_VAULT_SERVICE_ACCOUNT_TOKEN_PATH"),
		TokenRenewInterval:      1 * time.Hour,
		AllowPathPrefixs:        allowsArrays,
	}

	// Default to kubernetes auth if running in a pod (service account token exists)
	if cfg.AuthMethod == "" {
		if _, err := os.Stat(defaultServiceAccountTokenPath); err == nil {
			cfg.AuthMethod = "kubernetes"
			logger.Info("Auto-detected Kubernetes environment, using kubernetes auth method")
		} else {
			cfg.AuthMethod = "token"
			logger.Info("Using token auth method")
		}
	}

	// Set default role if not specified
	if cfg.KubernetesRole == "" && cfg.AuthMethod == "kubernetes" {
		cfg.KubernetesRole = "nitro-external-signer"
	}

	return cfg
}
