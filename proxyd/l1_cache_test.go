package proxyd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	dto "github.com/prometheus/client_model/go"
	"github.com/stretchr/testify/require"
)

var testL1Hash = "0x" + strings.Repeat("a", 64)
var testL1OtherHash = "0x" + strings.Repeat("b", 64)

func l1Req(method string, params any, id int) *RPCReq {
	return &RPCReq{JSONRPC: "2.0", Method: method, Params: mustMarshalJSON(params), ID: json.RawMessage(fmt.Sprint(id))}
}
func l1BlockRes(n uint64, h string) *RPCRes {
	return &RPCRes{JSONRPC: "2.0", Result: map[string]any{"number": fmt.Sprintf("0x%x", n), "hash": h}}
}
func testL1Cache(t *testing.T) *l1RPCCache {
	t.Helper()
	c, err := newL1RPCCache(L1CacheConfig{ChainID: 1})
	require.NoError(t, err)
	return c
}
func constantL1(res *RPCRes, calls *atomic.Int32) l1Forward {
	return func(context.Context, *RPCReq) (*RPCRes, string, error) { calls.Add(1); return res, "mock", nil }
}

func TestL1CacheConcurrentReadsAndWaiterCancellation(t *testing.T) {
	c := testL1Cache(t)
	started := make(chan struct{})
	release := make(chan struct{})
	var calls atomic.Int32
	load := func(ctx context.Context, r *RPCReq) (*RPCRes, string, error) {
		calls.Add(1)
		close(started)
		select {
		case <-release:
			return l1BlockRes(12, testL1Hash), "mock", nil
		case <-ctx.Done():
			return nil, "", ctx.Err()
		}
	}
	first, cancel := context.WithCancel(context.Background())
	firstDone := make(chan error, 1)
	go func() {
		_, _, _, err := c.Forward(first, "eth", l1Req("eth_getBlockByHash", []any{testL1Hash, true}, 1), load)
		firstDone <- err
	}()
	<-started
	joinedCounter := l1CacheEvents.WithLabelValues("eth_getBlockByHash", "coalesced")
	joinedValue := func() float64 {
		value := new(dto.Metric)
		require.NoError(t, joinedCounter.Write(value))
		return value.GetCounter().GetValue()
	}
	joinedBefore := joinedValue()
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for id := 2; id <= 3; id++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			r, _, _, err := c.Forward(context.Background(), "eth", l1Req("eth_getBlockByHash", []any{testL1Hash, true}, id), load)
			if err == nil && string(r.ID) != fmt.Sprint(id) {
				err = errors.New("wrong waiter ID")
			}
			errs <- err
		}(id)
	}
	require.Eventually(t, func() bool { return joinedValue() >= joinedBefore+2 },
		time.Second, time.Millisecond, "both followers must join the in-flight read before it completes")
	cancel()
	require.ErrorIs(t, <-firstDone, context.Canceled)
	close(release)
	wg.Wait()
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}
	require.EqualValues(t, 1, calls.Load())
	r, hit, _, err := c.Forward(context.Background(), "eth", l1Req("eth_getBlockByHash", []any{testL1Hash, true}, 99), load)
	require.NoError(t, err)
	require.True(t, hit)
	require.Equal(t, "99", string(r.ID))
}

func TestL1CacheReceiptCoverageAndBadResponses(t *testing.T) {
	for _, method := range []string{"eth_getBlockReceipts", "debug_getRawReceipts"} {
		t.Run(method, func(t *testing.T) {
			c := testL1Cache(t)
			var calls atomic.Int32
			var result any
			receipt := &types.Receipt{Type: types.LegacyTxType, Status: 1, CumulativeGasUsed: 21000, GasUsed: 21000,
				BlockHash: common.HexToHash(testL1Hash), TxHash: common.HexToHash(testL1OtherHash), BlockNumber: big.NewInt(1),
				Logs: []*types.Log{}, TransactionIndex: 0}
			if method == "debug_getRawReceipts" {
				raw, err := receipt.MarshalBinary()
				require.NoError(t, err)
				result = []string{fmt.Sprintf("0x%x", raw)}
			} else {
				result = []*types.Receipt{receipt}
			}
			load := constantL1(&RPCRes{Result: result}, &calls)
			for i := 0; i < 3; i++ {
				_, _, _, err := c.Forward(context.Background(), "eth", l1Req(method, []any{testL1Hash}, i), load)
				require.NoError(t, err)
			}
			require.EqualValues(t, 1, calls.Load())
		})
	}
	bad := []*RPCRes{{Result: nil}, {Result: json.RawMessage("null")}, {Result: []any{}}, {Error: &RPCErr{Code: -32005, Message: "quota"}}, {Result: []string{"not-hex"}}, {Result: []string{"0xc0"}}}
	for _, res := range bad {
		c := testL1Cache(t)
		var calls atomic.Int32
		load := constantL1(res, &calls)
		for i := 0; i < 2; i++ {
			_, _, _, err := c.Forward(context.Background(), "eth", l1Req("debug_getRawReceipts", []any{testL1Hash}, i), load)
			require.NoError(t, err)
		}
		require.EqualValues(t, 2, calls.Load())
	}
	c := testL1Cache(t)
	var calls atomic.Int32
	wrong := constantL1(l1BlockRes(1, testL1OtherHash), &calls)
	for i := 0; i < 2; i++ {
		_, _, _, err := c.Forward(context.Background(), "eth", l1Req("eth_getBlockByHash", []any{testL1Hash, false}, i), wrong)
		require.NoError(t, err)
	}
	require.EqualValues(t, 2, calls.Load(), "wrong hash must not poison cache")
}

func TestL1CacheFinalizedNumbersTagsAndRegression(t *testing.T) {
	c := testL1Cache(t)
	now := time.Unix(1000, 0)
	c.now = func() time.Time { return now }
	var calls atomic.Int32
	ctx := context.Background()
	number := l1Req("eth_getBlockByNumber", []any{"0x20", false}, 1)
	load := constantL1(l1BlockRes(32, testL1Hash), &calls)
	_, _, _, err := c.Forward(ctx, "eth", number, load)
	require.NoError(t, err)
	now = now.Add(31 * time.Second)
	_, hit, _, err := c.Forward(ctx, "eth", number, load)
	require.NoError(t, err)
	require.False(t, hit, "age/confirmation depth alone is not finality")
	finalized := l1Req("eth_getBlockByNumber", []any{"finalized", false}, 2)
	_, _, _, err = c.Forward(ctx, "eth", finalized, constantL1(l1BlockRes(100, testL1OtherHash), &calls))
	require.NoError(t, err)
	now = now.Add(31 * time.Second)
	_, _, _, err = c.Forward(ctx, "eth", number, load)
	require.NoError(t, err)
	now = now.Add(time.Hour)
	_, hit, _, err = c.Forward(ctx, "eth", number, load)
	require.NoError(t, err)
	require.True(t, hit, "fresh finalized number should have long TTL")
	_, hit, _, err = c.Forward(ctx, "eth", finalized, constantL1(l1BlockRes(100, testL1OtherHash), &calls))
	require.NoError(t, err)
	require.False(t, hit, "finalized label must keep moving")
	now = now.Add(31 * time.Second)
	_, _, _, err = c.Forward(ctx, "eth", finalized, constantL1(l1BlockRes(20, testL1OtherHash), &calls))
	require.NoError(t, err)
	_, hit, _, err = c.Forward(ctx, "eth", number, constantL1(l1BlockRes(32, testL1OtherHash), &calls))
	require.NoError(t, err)
	require.False(t, hit, "finality regression must invalidate canonical mappings")
	now = now.Add(31 * time.Second)
	_, _, _, err = c.Forward(ctx, "eth", finalized, constantL1(l1BlockRes(20, testL1Hash), &calls))
	require.NoError(t, err)
	_, hit, _, err = c.Forward(ctx, "eth", number, load)
	require.NoError(t, err)
	require.False(t, hit, "same-height finalized hash change must invalidate too")
}

func TestL1CacheExpirySizeAndCanonicalKeys(t *testing.T) {
	c, err := newL1RPCCache(L1CacheConfig{ChainID: 1, MaxBytes: 1024})
	require.NoError(t, err)
	now := time.Unix(1000, 0)
	c.now = func() time.Time { return now }
	var calls atomic.Int32
	load := constantL1(l1BlockRes(1, testL1Hash), &calls)
	req := l1Req("eth_getBlockByHash", []any{testL1Hash, true}, 1)
	_, _, _, err = c.Forward(context.Background(), "eth", req, load)
	require.NoError(t, err)
	equivalent := l1Req("eth_getBlockByHash", nil, 2)
	equivalent.Params = json.RawMessage(`[ "` + testL1Hash + `", true ]`)
	_, hit, _, err := c.Forward(context.Background(), "eth", equivalent, load)
	require.NoError(t, err)
	require.True(t, hit)
	header := l1Req("eth_getBlockByHash", []any{testL1Hash, false}, 3)
	_, hit, _, err = c.Forward(context.Background(), "eth", header, load)
	require.NoError(t, err)
	require.False(t, hit)
	require.LessOrEqual(t, c.bytes, c.cfg.MaxBytes)
	now = now.Add(7 * time.Hour)
	_, hit, _, err = c.Forward(context.Background(), "eth", header, load)
	require.NoError(t, err)
	require.False(t, hit)
	huge := l1BlockRes(1, testL1OtherHash)
	huge.Result.(map[string]any)["transactions"] = strings.Repeat("x", 2048)
	bigReq := l1Req("eth_getBlockByHash", []any{testL1OtherHash, true}, 4)
	for i := 0; i < 2; i++ {
		_, hit, _, err = c.Forward(context.Background(), "eth", bigReq, constantL1(huge, &calls))
		require.NoError(t, err)
		require.False(t, hit)
	}
	require.LessOrEqual(t, c.bytes, c.cfg.MaxBytes)
}

func TestL1CacheLimitsAndUncacheableReads(t *testing.T) {
	c, err := newL1RPCCache(L1CacheConfig{ChainID: 1, MaxInflight: 1, RequestTimeout: TOMLDuration(100 * time.Millisecond)})
	require.NoError(t, err)
	started := make(chan struct{})
	finished := make(chan error, 1)
	go func() {
		_, _, _, err := c.Forward(context.Background(), "eth", l1Req("eth_getBlockByHash", []any{testL1Hash, true}, 1), func(ctx context.Context, r *RPCReq) (*RPCRes, string, error) {
			close(started)
			<-ctx.Done()
			return nil, "", ctx.Err()
		})
		finished <- err
	}()
	<-started
	_, _, _, err = c.Forward(context.Background(), "eth", l1Req("eth_getBlockByHash", []any{testL1OtherHash, true}, 2), nil)
	require.ErrorContains(t, err, "in-flight limit")
	require.ErrorIs(t, <-finished, context.DeadlineExceeded)
	var calls atomic.Int32
	for _, req := range []*RPCReq{l1Req("eth_getTransactionReceipt", []any{testL1Hash}, 1), l1Req("eth_getBlockByNumber", []any{"pending", false}, 1), l1Req("eth_getBlockReceipts", []any{"0x1"}, 1)} {
		for i := 0; i < 2; i++ {
			_, hit, _, err := c.Forward(context.Background(), "eth", req, constantL1(&RPCRes{Result: "uncached"}, &calls))
			require.NoError(t, err)
			require.False(t, hit)
		}
	}
	require.EqualValues(t, 6, calls.Load())
}

func TestL1CacheServerBatchesAndLegacyPath(t *testing.T) {
	var calls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		var req RPCReq
		require.NoError(t, json.NewDecoder(r.Body).Decode(&req))
		res := l1BlockRes(12, testL1Hash)
		res.ID = req.ID
		require.NoError(t, json.NewEncoder(w).Encode(res))
	}))
	defer upstream.Close()
	be := NewBackend("mock", upstream.URL, "", nil)
	group := &BackendGroup{Name: "eth", Backends: []*Backend{be}, FallbackBackends: map[string]bool{"mock": false}, routingStrategy: FallbackRoutingStrategy}
	s := &Server{BackendGroups: map[string]*BackendGroup{"eth": group}, rpcMethodMappings: map[string]string{"eth_getBlockByHash": "eth"}, maxUpstreamBatchSize: 10, cache: &NoopRPCCache{}, l1Cache: testL1Cache(t), txFilter: NewTxFilter(nil)}
	req := l1Req("eth_getBlockByHash", []any{testL1Hash, true}, 7)
	req2 := l1Req("eth_getBlockByHash", []any{testL1Hash, true}, 9)
	batch := []json.RawMessage{mustMarshalJSON(req), mustMarshalJSON(req), mustMarshalJSON(req2)}
	res, _, _, err := s.handleBatchRPC(context.Background(), batch, func(string) bool { return false }, true, false)
	require.NoError(t, err)
	require.Len(t, res, 3)
	require.Equal(t, "7", string(res[0].ID))
	require.Equal(t, "7", string(res[1].ID))
	require.Equal(t, "9", string(res[2].ID))
	require.EqualValues(t, 1, calls.Load())
	// The default, non-L1 cache path still forwards an ordinary single request.
	s.l1Cache = nil
	_, _, _, err = s.handleBatchRPC(context.Background(), batch[:1], func(string) bool { return false }, false, false)
	require.NoError(t, err)
	require.EqualValues(t, 2, calls.Load())
}

func TestL1CacheRejectsUnsafeConfigurations(t *testing.T) {
	base := func() *Config {
		return &Config{Cache: CacheConfig{Enabled: true, L1: L1CacheConfig{Enabled: true, ChainID: 1}}, Backends: BackendsConfig{"upstream": &BackendConfig{}}, BackendGroups: BackendGroupsConfig{"eth": &BackendGroupConfig{Backends: []string{"upstream"}}}, RPCMethodMappings: map[string]string{"eth_chainId": "eth"}}
	}
	require.NoError(t, validateL1CacheConfig(base()))
	for _, change := range []func(*Config){func(c *Config) { c.Redis.URL = "redis://localhost" }, func(c *Config) { c.Server.WSPort = 8081 }, func(c *Config) { c.Cache.Enabled = false }, func(c *Config) { c.Cache.L1.ChainID = 0 }, func(c *Config) { c.BackendGroups["eth"].ConsensusAware = true }, func(c *Config) { c.RPCMethodMappings["eth_sendRawTransaction"] = "eth" }, func(c *Config) { c.Cache.L1.NumberTTL = TOMLDuration(time.Hour) }} {
		c := base()
		change(c)
		require.Error(t, validateL1CacheConfig(c))
	}
	require.NoError(t, validateL1CacheConfig(&Config{}), "legacy configuration is untouched")
}

func TestL1CacheFencesNumberReadAcrossFinalityChange(t *testing.T) {
	c := testL1Cache(t)
	var calls atomic.Int32
	ctx := context.Background()
	_, _, _, err := c.Forward(ctx, "eth", l1Req("eth_getBlockByNumber", []any{"finalized", false}, 1), constantL1(l1BlockRes(100, testL1Hash), &calls))
	require.NoError(t, err)
	started := make(chan struct{})
	release := make(chan struct{})
	done := make(chan error, 1)
	number := l1Req("eth_getBlockByNumber", []any{"0x10", false}, 2)
	go func() {
		_, _, _, err := c.Forward(ctx, "eth", number, func(context.Context, *RPCReq) (*RPCRes, string, error) {
			close(started)
			<-release
			return l1BlockRes(16, testL1Hash), "mock", nil
		})
		done <- err
	}()
	<-started
	// A distinct fullTx parameter observes a newer boundary without using the tag cache.
	_, _, _, err = c.Forward(ctx, "eth", l1Req("eth_getBlockByNumber", []any{"finalized", true}, 3), constantL1(l1BlockRes(200, testL1OtherHash), &calls))
	require.NoError(t, err)
	close(release)
	require.NoError(t, <-done)
	_, hit, _, err := c.Forward(ctx, "eth", number, constantL1(l1BlockRes(16, testL1OtherHash), &calls))
	require.NoError(t, err)
	require.False(t, hit, "an older in-flight number mapping must not be promoted after the boundary changed")
}

func TestL1CacheTransportErrorRedaction(t *testing.T) {
	upstream := &url.Error{Op: "Post", URL: "https://example.invalid/v1/private-token", Err: context.DeadlineExceeded}
	backend := &Backend{redactRPCErrors: true}
	err := backend.rpcRequestError(upstream, "request")
	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.NotContains(t, err.Error(), "private-token")
	require.NotContains(t, err.Error(), "example.invalid")
	backend.redactRPCErrors = false
	require.Contains(t, backend.rpcRequestError(upstream, "request").Error(), "private-token", "legacy behavior remains unchanged")
}
