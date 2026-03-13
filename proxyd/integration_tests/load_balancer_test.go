package integration_tests

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path"
	"testing"

	"github.com/ethereum-optimism/infra/proxyd"
	ms "github.com/ethereum-optimism/infra/proxyd/tools/mockserver/handler"
	"github.com/stretchr/testify/require"
)

type lbNodeContext struct {
	backend     *proxyd.Backend
	mockBackend *MockBackend
	handler     *ms.MockedHandler
}

func setupLoadBalancer(t *testing.T) (map[string]lbNodeContext, *proxyd.BackendGroup, *ProxydHTTPClient, func()) {
	node1 := NewMockBackend(nil)
	node2 := NewMockBackend(nil)
	node3 := NewMockBackend(nil)

	dir, err := os.Getwd()
	require.NoError(t, err)

	responses := path.Join(dir, "testdata/load_balancer_responses.yml")

	h1 := ms.MockedHandler{Overrides: []*ms.MethodTemplate{}, Autoload: true, AutoloadFile: responses}
	h2 := ms.MockedHandler{Overrides: []*ms.MethodTemplate{}, Autoload: true, AutoloadFile: responses}
	h3 := ms.MockedHandler{Overrides: []*ms.MethodTemplate{}, Autoload: true, AutoloadFile: responses}

	require.NoError(t, os.Setenv("NODE1_URL", node1.URL()))
	require.NoError(t, os.Setenv("NODE2_URL", node2.URL()))
	require.NoError(t, os.Setenv("NODE3_URL", node3.URL()))

	node1.SetHandler(http.HandlerFunc(h1.Handler))
	node2.SetHandler(http.HandlerFunc(h2.Handler))
	node3.SetHandler(http.HandlerFunc(h3.Handler))

	config := ReadConfig("load_balancer")
	svr, shutdown, err := proxyd.Start(config)
	require.NoError(t, err)

	client := NewProxydClient("http://127.0.0.1:8545")

	bg := svr.BackendGroups["node"]
	require.NotNil(t, bg)
	require.NotNil(t, bg.Consensus)
	require.Equal(t, 3, len(bg.Backends))

	nodes := map[string]lbNodeContext{
		"node1": {mockBackend: node1, backend: bg.Backends[0], handler: &h1},
		"node2": {mockBackend: node2, backend: bg.Backends[1], handler: &h2},
		"node3": {mockBackend: node3, backend: bg.Backends[2], handler: &h3},
	}

	return nodes, bg, client, shutdown
}

func TestLoadBalancer(t *testing.T) {
	nodes, bg, _, shutdown := setupLoadBalancer(t)
	defer nodes["node1"].mockBackend.Close()
	defer nodes["node2"].mockBackend.Close()
	defer nodes["node3"].mockBackend.Close()
	defer shutdown()

	ctx := context.Background()

	update := func() {
		for _, be := range bg.Backends {
			bg.Consensus.UpdateBackend(ctx, be)
		}
		bg.Consensus.UpdateBackendGroupConsensus(ctx)
	}

	// After initial update, all 3 nodes should be in the consensus group
	update()

	group := bg.Consensus.GetConsensusGroup()
	require.Equal(t, 3, len(group), "all 3 healthy backends should be in load balancer group")

	// Verify it's running in relaxed mode (load_balancer strategy)
	require.Equal(t, proxyd.LoadBalancerRoutingStrategy, bg.GetRoutingStrategy())
}

func TestLoadBalancer_StickySession(t *testing.T) {
	nodes, bg, _, shutdown := setupLoadBalancer(t)
	defer nodes["node1"].mockBackend.Close()
	defer nodes["node2"].mockBackend.Close()
	defer nodes["node3"].mockBackend.Close()
	defer shutdown()

	ctx := context.Background()

	update := func() {
		for _, be := range bg.Backends {
			bg.Consensus.UpdateBackend(ctx, be)
		}
		bg.Consensus.UpdateBackendGroupConsensus(ctx)
	}
	update()

	// Create two clients with different XFF headers
	h1 := make(http.Header)
	h1.Set("X-Forwarded-For", "192.168.1.100")
	client1 := NewProxydClientWithHeaders("http://127.0.0.1:8545", h1)

	h2 := make(http.Header)
	h2.Set("X-Forwarded-For", "10.0.0.50")
	client2 := NewProxydClientWithHeaders("http://127.0.0.1:8545", h2)

	// Send multiple requests from client1 and track which backend handles them
	for i := 0; i < 5; i++ {
		nodes["node1"].mockBackend.Reset()
		nodes["node2"].mockBackend.Reset()
		nodes["node3"].mockBackend.Reset()

		_, code, err := client1.SendRPC("eth_chainId", nil)
		require.NoError(t, err)
		require.Equal(t, 200, code)
	}

	// Same IP should consistently route to the same backend
	// We verify by sending more requests and checking that the pattern is stable
	nodes["node1"].mockBackend.Reset()
	nodes["node2"].mockBackend.Reset()
	nodes["node3"].mockBackend.Reset()

	numRequests := 10
	for i := 0; i < numRequests; i++ {
		_, code, err := client1.SendRPC("eth_chainId", nil)
		require.NoError(t, err)
		require.Equal(t, 200, code)
	}

	// Count requests per backend for client1
	c1n1 := len(nodes["node1"].mockBackend.Requests())
	c1n2 := len(nodes["node2"].mockBackend.Requests())
	c1n3 := len(nodes["node3"].mockBackend.Requests())

	// With sticky session, all requests from the same IP should go to one backend
	// (the poller also makes requests, so we check that one backend got all the client requests)
	maxCount := max(c1n1, c1n2, c1n3)
	require.Equal(t, numRequests, maxCount,
		"all requests from the same IP should route to the same backend, got node1=%d node2=%d node3=%d",
		c1n1, c1n2, c1n3)

	// Send requests from client2 (different IP)
	nodes["node1"].mockBackend.Reset()
	nodes["node2"].mockBackend.Reset()
	nodes["node3"].mockBackend.Reset()

	for i := 0; i < numRequests; i++ {
		_, code, err := client2.SendRPC("eth_chainId", nil)
		require.NoError(t, err)
		require.Equal(t, 200, code)
	}

	c2n1 := len(nodes["node1"].mockBackend.Requests())
	c2n2 := len(nodes["node2"].mockBackend.Requests())
	c2n3 := len(nodes["node3"].mockBackend.Requests())

	maxCount2 := max(c2n1, c2n2, c2n3)
	require.Equal(t, numRequests, maxCount2,
		"all requests from client2 should also route to one backend, got node1=%d node2=%d node3=%d",
		c2n1, c2n2, c2n3)
}

func TestLoadBalancer_NoBlockTagRewriting(t *testing.T) {
	nodes, bg, _, shutdown := setupLoadBalancer(t)
	defer nodes["node1"].mockBackend.Close()
	defer nodes["node2"].mockBackend.Close()
	defer nodes["node3"].mockBackend.Close()
	defer shutdown()

	ctx := context.Background()

	update := func() {
		for _, be := range bg.Backends {
			bg.Consensus.UpdateBackend(ctx, be)
		}
		bg.Consensus.UpdateBackendGroupConsensus(ctx)
	}
	update()

	h := make(http.Header)
	h.Set("X-Forwarded-For", "192.168.1.1")
	client := NewProxydClientWithHeaders("http://127.0.0.1:8545", h)

	// Send eth_blockNumber request — in consensus_aware mode this would return the consensus
	// block number, but in load_balancer mode the request should pass through unmodified
	_, code, err := client.SendRPC("eth_blockNumber", nil)
	require.NoError(t, err)
	require.Equal(t, 200, code)

	// Send eth_getBlockByNumber with "latest" — should NOT be rewritten to a specific block number
	_, code, err = client.SendRPC("eth_getBlockByNumber", []interface{}{"latest", false})
	require.NoError(t, err)
	require.Equal(t, 200, code)
}

func TestLoadBalancer_Failover(t *testing.T) {
	nodes, bg, _, shutdown := setupLoadBalancer(t)
	defer nodes["node1"].mockBackend.Close()
	defer nodes["node2"].mockBackend.Close()
	defer nodes["node3"].mockBackend.Close()
	defer shutdown()

	ctx := context.Background()

	update := func() {
		for _, be := range bg.Backends {
			bg.Consensus.UpdateBackend(ctx, be)
		}
		bg.Consensus.UpdateBackendGroupConsensus(ctx)
	}
	update()

	h := make(http.Header)
	h.Set("X-Forwarded-For", "172.16.0.1")
	client := NewProxydClientWithHeaders("http://127.0.0.1:8545", h)

	// Verify initial state — all 3 backends in group
	group := bg.Consensus.GetConsensusGroup()
	require.Equal(t, 3, len(group))

	// Figure out which backend this IP routes to
	nodes["node1"].mockBackend.Reset()
	nodes["node2"].mockBackend.Reset()
	nodes["node3"].mockBackend.Reset()

	_, code, err := client.SendRPC("eth_chainId", nil)
	require.NoError(t, err)
	require.Equal(t, 200, code)

	// Find primary backend
	var primaryNode string
	for name, node := range nodes {
		if len(node.mockBackend.Requests()) > 0 {
			primaryNode = name
			break
		}
	}
	require.NotEmpty(t, primaryNode, "should have found a primary backend")

	// Ban the primary backend to simulate failure
	bg.Consensus.Ban(nodes[primaryNode].backend)
	update()

	// Verify the group now has only 2 backends
	group = bg.Consensus.GetConsensusGroup()
	require.Equal(t, 2, len(group))

	// Verify we can still serve requests (failover to another backend)
	_, code, err = client.SendRPC("eth_chainId", nil)
	require.NoError(t, err)
	require.Equal(t, 200, code)
}

func TestLoadBalancer_MaxBlockRange(t *testing.T) {
	nodes, bg, _, shutdown := setupLoadBalancer(t)
	defer nodes["node1"].mockBackend.Close()
	defer nodes["node2"].mockBackend.Close()
	defer nodes["node3"].mockBackend.Close()
	defer shutdown()

	ctx := context.Background()

	update := func() {
		for _, be := range bg.Backends {
			bg.Consensus.UpdateBackend(ctx, be)
		}
		bg.Consensus.UpdateBackendGroupConsensus(ctx)
	}
	update()

	h := make(http.Header)
	h.Set("X-Forwarded-For", "192.168.1.1")
	client := NewProxydClientWithHeaders("http://127.0.0.1:8545", h)

	// eth_getLogs with a range within the limit (100 blocks) should succeed
	resBody, code, err := client.SendRPC("eth_getLogs", []interface{}{
		map[string]interface{}{
			"fromBlock": "0x1",
			"toBlock":   "0x65", // range = 0x65 - 0x1 = 100, exactly at limit
		},
	})
	require.NoError(t, err)
	require.Equal(t, 200, code)

	// Verify no error in response
	var res proxyd.RPCRes
	require.NoError(t, json.Unmarshal(resBody, &res))
	require.Nil(t, res.Error, "request within block range should succeed")

	// eth_getLogs with a range exceeding the limit should return an error
	resBody, code, err = client.SendRPC("eth_getLogs", []interface{}{
		map[string]interface{}{
			"fromBlock": "0x1",
			"toBlock":   "0x66", // range = 0x66 - 0x1 = 101, exceeds limit of 100
		},
	})
	require.NoError(t, err)
	require.Equal(t, 400, code)

	require.NoError(t, json.Unmarshal(resBody, &res))
	require.NotNil(t, res.Error, "request exceeding block range should return error")
	require.Contains(t, res.Error.Message, "block range greater than 100 max")
}

func max(a, b, c int) int {
	if a >= b && a >= c {
		return a
	}
	if b >= a && b >= c {
		return b
	}
	return c
}
