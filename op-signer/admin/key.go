package admin

// KeyConfig defines the configuration for retrieving and using a private key from Vault
type KeyConfig struct {
	Path string `json:"path"` // Vault path to the key (e.g., "secret/data/my-key")

	// ParentChainID is REQUIRED for go-ethereum v1.16.8+ compatibility.
	// In v1.16.8, creating a signer with chainID <= 0 causes a PANIC (newModernSigner validation).
	// Nitro BOLD staker sends transactions with chainID=0 due to bugs in TransactOpts handling.
	// This field provides the correct chainID to use when we receive chainID=0.
	// Example: "421614" for Arbitrum Sepolia
	ParentChainID string `json:"parent_chain_id"`

	// AllowedClientCN (optional) restricts signing to a specific client certificate Common Name.
	// If set, only requests from clients presenting a certificate with this CN can sign with this key.
	// If empty, any authenticated client can use this key.
	// Example: "orbit-demo-testnet-batchposter-0"
	AllowedClientCN string `json:"allowed_client_cn"`
}
