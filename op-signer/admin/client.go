package admin

import (
	"bytes"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"sync/atomic"
)

// jsonRPCRequest represents a JSON-RPC 2.0 request
type jsonRPCRequest struct {
	JSONRPC string `json:"jsonrpc"`
	Method  string `json:"method"`
	Params  []any  `json:"params"`
	ID      uint64 `json:"id"`
}

// jsonRPCResponse represents a JSON-RPC 2.0 response
type jsonRPCResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *jsonRPCError   `json:"error,omitempty"`
	ID      uint64          `json:"id"`
}

// jsonRPCError represents a JSON-RPC 2.0 error
type jsonRPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (e *jsonRPCError) Error() string {
	return fmt.Sprintf("rpc error (code %d): %s", e.Code, e.Message)
}

// AdminClient wraps HTTP calls to the admin JSON-RPC API
type AdminClient struct {
	endpoint string
	password string
	client   *http.Client
	nextID   atomic.Uint64
}

// NewAdminClient creates an AdminClient with TLS and basic auth configured.
// If tlsCert/tlsKey are empty, no client certificate is used.
// If caCert is empty, the system CA pool is used.
func NewAdminClient(endpoint, password, tlsCert, tlsKey, caCert string) (*AdminClient, error) {
	tlsConfig := &tls.Config{}

	if tlsCert != "" && tlsKey != "" {
		cert, err := tls.LoadX509KeyPair(tlsCert, tlsKey)
		if err != nil {
			return nil, fmt.Errorf("loading client cert/key: %w", err)
		}
		tlsConfig.Certificates = []tls.Certificate{cert}
	}

	if caCert != "" {
		caCertPEM, err := os.ReadFile(caCert)
		if err != nil {
			return nil, fmt.Errorf("reading CA cert: %w", err)
		}
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(caCertPEM) {
			return nil, fmt.Errorf("failed to parse CA cert")
		}
		tlsConfig.RootCAs = pool
	}

	return &AdminClient{
		endpoint: endpoint,
		password: password,
		client: &http.Client{
			Transport: &http.Transport{
				TLSClientConfig: tlsConfig,
			},
		},
	}, nil
}

// call sends a JSON-RPC 2.0 request and returns the result
func (c *AdminClient) call(method string, params []any) (json.RawMessage, error) {
	id := c.nextID.Add(1)
	reqBody := jsonRPCRequest{
		JSONRPC: "2.0",
		Method:  method,
		Params:  params,
		ID:      id,
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshaling request: %w", err)
	}

	req, err := http.NewRequest(http.MethodPost, c.endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.SetBasicAuth("", c.password)

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("sending request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(respBody))
	}

	var rpcResp jsonRPCResponse
	if err := json.Unmarshal(respBody, &rpcResp); err != nil {
		return nil, fmt.Errorf("unmarshaling response: %w", err)
	}

	if rpcResp.Error != nil {
		return nil, rpcResp.Error
	}

	return rpcResp.Result, nil
}

// GetConfigs returns all key configurations
func (c *AdminClient) GetConfigs() (map[string]KeyConfig, error) {
	result, err := c.call("admin_getConfigs", nil)
	if err != nil {
		return nil, err
	}
	var configs map[string]KeyConfig
	if err := json.Unmarshal(result, &configs); err != nil {
		return nil, fmt.Errorf("unmarshaling configs: %w", err)
	}
	return configs, nil
}

// AddConfig adds a new key configuration for the given address
func (c *AdminClient) AddConfig(address string, keyConfig KeyConfig) (string, error) {
	result, err := c.call("admin_addConfig", []any{address, keyConfig})
	if err != nil {
		return "", err
	}
	var msg string
	if err := json.Unmarshal(result, &msg); err != nil {
		return "", fmt.Errorf("unmarshaling result: %w", err)
	}
	return msg, nil
}

// RemoveConfig removes the key configuration for the given address
func (c *AdminClient) RemoveConfig(address string) (string, error) {
	result, err := c.call("admin_removeConfig", []any{address})
	if err != nil {
		return "", err
	}
	var msg string
	if err := json.Unmarshal(result, &msg); err != nil {
		return "", fmt.Errorf("unmarshaling result: %w", err)
	}
	return msg, nil
}

// GetConfigForAddress returns the key configuration for the given address
func (c *AdminClient) GetConfigForAddress(address string) (*KeyConfig, error) {
	result, err := c.call("admin_getConfigForAddress", []any{address})
	if err != nil {
		return nil, err
	}
	var config KeyConfig
	if err := json.Unmarshal(result, &config); err != nil {
		return nil, fmt.Errorf("unmarshaling config: %w", err)
	}
	return &config, nil
}

// GetConfigForPath returns the key configuration for the given vault path
func (c *AdminClient) GetConfigForPath(path string) (*KeyConfig, error) {
	result, err := c.call("admin_getConfigForPath", []any{path})
	if err != nil {
		return nil, err
	}
	var config KeyConfig
	if err := json.Unmarshal(result, &config); err != nil {
		return nil, fmt.Errorf("unmarshaling config: %w", err)
	}
	return &config, nil
}
