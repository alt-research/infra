package admin

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
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

// AdminClient wraps HTTP calls to the admin JSON-RPC API.
// The admin port is localhost-only with no authentication required.
type AdminClient struct {
	endpoint string
	client   *http.Client
	nextID   atomic.Uint64
}

// NewAdminClient creates an AdminClient for the given endpoint.
func NewAdminClient(endpoint string) *AdminClient {
	return &AdminClient{
		endpoint: endpoint,
		client:   &http.Client{},
	}
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

// GetConfigs returns all key configurations including derived Ethereum addresses.
func (c *AdminClient) GetConfigs() ([]KeyConfig, error) {
	result, err := c.call("admin_getConfigs", nil)
	if err != nil {
		return nil, err
	}
	var configs []KeyConfig
	if err := json.Unmarshal(result, &configs); err != nil {
		return nil, fmt.Errorf("unmarshaling configs: %w", err)
	}
	return configs, nil
}
