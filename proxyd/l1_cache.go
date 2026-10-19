package proxyd

import (
	"bytes"
	"container/list"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/core/types"
)

// L1CacheConfig opts a dedicated, single-backend HTTP proxy into shared L1 reads.
// It deliberately does not use Redis or infer finality from confirmation depth.
type L1CacheConfig struct {
	Enabled        bool         `toml:"enabled"`
	ChainID        uint64       `toml:"chain_id"`
	MaxBytes       int64        `toml:"max_bytes"`
	MaxInflight    int          `toml:"max_inflight"`
	ImmutableTTL   TOMLDuration `toml:"immutable_ttl"`
	TagTTL         TOMLDuration `toml:"tag_ttl"`
	NumberTTL      TOMLDuration `toml:"number_ttl"`
	RequestTimeout TOMLDuration `toml:"request_timeout"`
}

func (c L1CacheConfig) defaults() (L1CacheConfig, error) {
	if c.ChainID == 0 {
		return c, errors.New("cache.l1.chain_id must be positive")
	}
	if c.MaxBytes == 0 {
		c.MaxBytes = 256 << 20
	}
	if c.MaxInflight == 0 {
		c.MaxInflight = 16
	}
	if c.ImmutableTTL == 0 {
		c.ImmutableTTL = TOMLDuration(6 * time.Hour)
	}
	if c.TagTTL == 0 {
		c.TagTTL = TOMLDuration(30 * time.Second)
	}
	if c.NumberTTL == 0 {
		c.NumberTTL = TOMLDuration(30 * time.Second)
	}
	if c.RequestTimeout == 0 {
		c.RequestTimeout = TOMLDuration(20 * time.Second)
	}
	if c.MaxBytes < 1024 || c.MaxBytes > 4<<30 || c.MaxInflight < 1 || c.MaxInflight > 256 {
		return c, errors.New("cache.l1 requires max_bytes between 1 KiB and 4 GiB and max_inflight between 1 and 256")
	}
	if c.ImmutableTTL < 0 || c.TagTTL < 0 || c.NumberTTL < 0 || c.RequestTimeout < 0 {
		return c, errors.New("cache.l1 durations must be positive")
	}
	if time.Duration(c.TagTTL) > 5*time.Minute || time.Duration(c.NumberTTL) > 5*time.Minute || time.Duration(c.RequestTimeout) > time.Minute {
		return c, errors.New("cache.l1 mutable TTLs must not exceed 5m and request_timeout must not exceed 1m")
	}
	return c, nil
}

var l1ReadMethods = map[string]bool{
	"eth_chainId": true, "net_version": true, "web3_clientVersion": true, "eth_syncing": true, "net_peerCount": true,
	"eth_blockNumber": true, "eth_getBlockByHash": true, "eth_getBlockByNumber": true,
	"eth_getBlockReceipts": true, "debug_getRawReceipts": true, "eth_getTransactionReceipt": true,
	"eth_getStorageAt": true, "eth_getProof": true, "eth_getLogs": true, "eth_getTransactionByHash": true,
	"eth_getBlockTransactionCountByHash": true, "eth_getBlockTransactionCountByNumber": true,
}

func validateL1CacheConfig(c *Config) error {
	if !c.Cache.L1.Enabled {
		return nil
	}
	if !c.Cache.Enabled || c.Redis.URL != "" || c.Redis.ReadURL != "" {
		return errors.New("cache.l1 requires cache.enabled and no Redis")
	}
	if len(c.Backends) != 1 || len(c.BackendGroups) != 1 || c.Server.WSPort != 0 || c.WSBackendGroup != "" {
		return errors.New("cache.l1 requires one backend, one fallback group, and HTTP only")
	}
	for _, g := range c.BackendGroups {
		if len(g.Backends) != 1 || g.ConsensusAware || (g.RoutingStrategy != "" && g.RoutingStrategy != FallbackRoutingStrategy) {
			return errors.New("cache.l1 requires single-backend fallback routing without consensus rewriting")
		}
	}
	for method := range c.RPCMethodMappings {
		if !l1ReadMethods[method] {
			return fmt.Errorf("cache.l1 does not allow method %q", method)
		}
	}
	cfg, err := c.Cache.L1.defaults()
	if err != nil {
		return err
	}
	c.Cache.L1 = cfg
	return nil
}

type l1Read struct {
	key, method, selector string
	number                uint64
	kind                  string
}
type l1Entry struct {
	key     string
	result  json.RawMessage
	expires time.Time
	cost    int64
	mutable bool
}
type l1Head struct {
	number uint64
	hash   string
}
type l1Flight struct {
	done     chan struct{}
	response *RPCRes
	servedBy string
	err      error
}
type l1Forward func(context.Context, *RPCReq) (*RPCRes, string, error)

// All retained entries are charged for JSON payload, key and a metadata allowance.
// MaxBytes is not a process RSS limit; in-flight responses and Go overhead are separate.
type l1RPCCache struct {
	cfg         L1CacheConfig
	mu          sync.Mutex
	entries     map[string]*list.Element
	order       *list.List
	bytes       int64
	flights     map[string]*l1Flight
	finalized   map[string]l1Head
	generations map[string]uint64
	now         func() time.Time
}

func newL1RPCCache(cfg L1CacheConfig) (*l1RPCCache, error) {
	cfg, err := cfg.defaults()
	if err != nil {
		return nil, err
	}
	return &l1RPCCache{cfg: cfg, entries: make(map[string]*list.Element), order: list.New(), flights: make(map[string]*l1Flight), finalized: make(map[string]l1Head), generations: make(map[string]uint64), now: time.Now}, nil
}

func l1Hash(s string) (string, bool) {
	if len(s) != 66 || !strings.HasPrefix(s, "0x") {
		return "", false
	}
	if _, err := hex.DecodeString(s[2:]); err != nil {
		return "", false
	}
	return strings.ToLower(s), true
}
func l1Quantity(s string) (uint64, bool) {
	if !strings.HasPrefix(s, "0x") || len(s) < 3 {
		return 0, false
	}
	n, err := strconv.ParseUint(s[2:], 16, 64)
	return n, err == nil
}

func (c *l1RPCCache) read(group string, req *RPCReq) (l1Read, bool) {
	d := l1Read{method: req.Method}
	var p []json.RawMessage
	if len(req.Params) > 0 && json.Unmarshal(req.Params, &p) != nil {
		return d, false
	}
	var normalized any
	switch req.Method {
	case "eth_chainId", "net_version":
		if len(p) != 0 {
			return d, false
		}
		d.kind = "identity"
		normalized = []any{}
	case "eth_getBlockByHash", "eth_getBlockByNumber":
		if len(p) != 2 {
			return d, false
		}
		var sel string
		var full bool
		if json.Unmarshal(p[0], &sel) != nil || string(p[1]) == "null" || json.Unmarshal(p[1], &full) != nil {
			return d, false
		}
		if req.Method == "eth_getBlockByHash" {
			h, ok := l1Hash(sel)
			if !ok {
				return d, false
			}
			d.kind = "hash-block"
			d.selector = h
		} else if sel == "latest" || sel == "safe" || sel == "finalized" {
			d.kind = "tag"
			d.selector = sel
		} else {
			n, ok := l1Quantity(sel)
			if !ok {
				return d, false
			}
			d.kind = "number"
			d.number = n
			d.selector = fmt.Sprintf("0x%x", n)
		}
		normalized = []any{d.selector, full}
	case "eth_getBlockReceipts", "debug_getRawReceipts":
		if len(p) != 1 {
			return d, false
		}
		var sel string
		if json.Unmarshal(p[0], &sel) != nil {
			return d, false
		}
		h, ok := l1Hash(sel)
		if !ok {
			return d, false
		}
		d.selector = h
		d.kind = "receipts"
		normalized = []any{h}
	default:
		return d, false
	}
	b, _ := json.Marshal(normalized)
	// Group isolates backend selection even when the cache is constructed directly in tests.
	d.key = fmt.Sprintf("%d/%s/%s/%s", c.cfg.ChainID, group, req.Method, b)
	return d, true
}

func cloneL1Response(res *RPCRes, req *RPCReq) *RPCRes {
	if res == nil {
		return nil
	}
	out := *res
	out.ID = append(json.RawMessage(nil), req.ID...)
	out.JSONRPC = req.JSONRPC
	if res.Result != nil {
		b, err := json.Marshal(res.Result)
		if err == nil {
			out.Result = json.RawMessage(b)
		}
	}
	if res.Error != nil {
		e := *res.Error
		e.Data = append(json.RawMessage(nil), e.Data...)
		out.Error = &e
	}
	return &out
}

func (c *l1RPCCache) remove(el *list.Element) {
	e := el.Value.(*l1Entry)
	delete(c.entries, e.key)
	c.bytes -= e.cost
	c.order.Remove(el)
	l1CacheBytes.Set(float64(c.bytes))
}
func (c *l1RPCCache) getLocked(d l1Read, req *RPCReq) *RPCRes {
	el := c.entries[d.key]
	if el == nil {
		return nil
	}
	e := el.Value.(*l1Entry)
	if !c.now().Before(e.expires) {
		c.remove(el)
		return nil
	}
	c.order.MoveToFront(el)
	return &RPCRes{JSONRPC: req.JSONRPC, ID: append(json.RawMessage(nil), req.ID...), Result: append(json.RawMessage(nil), e.result...)}
}

// Forward shares both a finished response and a bounded in-flight operation.
// A waiter's cancellation does not cancel other waiters. Detached work is bounded
// by RequestTimeout and MaxInflight; it never carries on as an unbounded retry.
func (c *l1RPCCache) Forward(ctx context.Context, group string, req *RPCReq, forward l1Forward) (*RPCRes, bool, string, error) {
	if err := ctx.Err(); err != nil {
		return nil, false, "", err
	}
	d, ok := c.read(group, req)
	if !ok {
		l1CacheEvents.WithLabelValues(req.Method, "bypass").Inc()
		res, sb, err := forward(ctx, req)
		return res, false, sb, err
	}
	c.mu.Lock()
	if res := c.getLocked(d, req); res != nil {
		c.mu.Unlock()
		RecordCacheHit(req.Method)
		return res, true, "", nil
	}
	f := c.flights[d.key]
	shared := f != nil
	if f == nil {
		if len(c.flights) >= c.cfg.MaxInflight {
			c.mu.Unlock()
			return nil, false, "", errors.New("L1 cache in-flight limit reached")
		}
		f = &l1Flight{done: make(chan struct{})}
		c.flights[d.key] = f
		generation := c.generations[group]
		input := *req
		input.Params = append(json.RawMessage(nil), req.Params...)
		input.ID = append(json.RawMessage(nil), req.ID...)
		workCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), time.Duration(c.cfg.RequestTimeout))
		go func() {
			defer cancel()
			res, sb, err := forward(workCtx, &input)
			snapshot := cloneL1Response(res, &input)
			c.mu.Lock()
			defer c.mu.Unlock()
			if err == nil && snapshot != nil {
				c.putLocked(group, d, snapshot, generation)
			}
			f.response = snapshot
			f.servedBy = sb
			f.err = err
			delete(c.flights, d.key)
			close(f.done)
		}()
	}
	c.mu.Unlock()
	if shared {
		l1CacheEvents.WithLabelValues(req.Method, "coalesced").Inc()
	} else {
		RecordCacheMiss(req.Method)
	}
	select {
	case <-ctx.Done():
		return nil, false, "", ctx.Err()
	case <-f.done:
		return cloneL1Response(f.response, req), shared, f.servedBy, f.err
	}
}

func l1Block(b []byte) (l1Head, bool) {
	var v struct {
		Number string `json:"number"`
		Hash   string `json:"hash"`
	}
	if json.Unmarshal(b, &v) != nil {
		return l1Head{}, false
	}
	n, ok := l1Quantity(v.Number)
	h, valid := l1Hash(v.Hash)
	return l1Head{n, h}, ok && valid
}

func (c *l1RPCCache) putLocked(group string, d l1Read, res *RPCRes, generation uint64) {
	if res.Error != nil || res.Result == nil {
		return
	}
	data, err := json.Marshal(res.Result)
	if err != nil || bytes.Equal(bytes.TrimSpace(data), []byte("null")) {
		return
	}
	ttl := time.Duration(c.cfg.ImmutableTTL)
	mutable := false
	switch d.kind {
	case "identity":
		var id string
		if json.Unmarshal(data, &id) != nil {
			return
		}
		if d.method == "eth_chainId" {
			n, ok := l1Quantity(id)
			if !ok || n != c.cfg.ChainID {
				return
			}
		} else if id != strconv.FormatUint(c.cfg.ChainID, 10) {
			return
		}
	case "hash-block", "number", "tag":
		head, ok := l1Block(data)
		if !ok {
			return
		}
		if d.kind == "hash-block" && head.hash != d.selector {
			return
		}
		if d.kind == "number" && head.number != d.number {
			return
		}
		if d.kind != "hash-block" {
			mutable = true
			// Do not insert a result started before a finality contradiction was observed.
			if c.generations[group] != generation {
				return
			}
		}
		if d.kind == "tag" {
			ttl = time.Duration(c.cfg.TagTTL)
			if d.selector == "finalized" {
				old, exists := c.finalized[group]
				if exists && (head.number < old.number || (head.number == old.number && head.hash != old.hash)) {
					c.generations[group]++
					// A finality contradiction invalidates canonical-number and tag mappings.
					for _, el := range c.entries {
						if el.Value.(*l1Entry).mutable {
							c.remove(el)
						}
					}
					l1CacheEvents.WithLabelValues(d.method, "finality_reset").Inc()
				}
				// Also fence reads started before the first/next finality observation:
				// they must not be promoted using a newer canonical boundary.
				if !exists || head != old {
					c.generations[group]++
				}
				c.finalized[group] = head
			}
		} else if d.kind == "number" {
			f, observed := c.finalized[group]
			if !observed || head.number > f.number {
				ttl = time.Duration(c.cfg.NumberTTL)
			}
			// At the boundary the canonical hash can be checked without another RPC.
			if observed && head.number == f.number && head.hash != f.hash {
				return
			}
		}
	case "receipts":
		var rows []json.RawMessage
		if json.Unmarshal(data, &rows) != nil || len(rows) == 0 {
			return
		}
		for i, row := range rows {
			if d.method == "debug_getRawReceipts" {
				var raw string
				if json.Unmarshal(row, &raw) != nil || !strings.HasPrefix(raw, "0x") || len(raw) < 4 {
					return
				}
				encoded, err := hex.DecodeString(raw[2:])
				if err != nil {
					return
				}
				var decoded types.Receipt
				if decoded.UnmarshalBinary(encoded) != nil {
					return
				}
			} else {
				var receipt struct {
					BlockHash        string `json:"blockHash"`
					TransactionHash  string `json:"transactionHash"`
					TransactionIndex string `json:"transactionIndex"`
				}
				if json.Unmarshal(row, &receipt) != nil {
					return
				}
				h, ok := l1Hash(receipt.BlockHash)
				_, txOK := l1Hash(receipt.TransactionHash)
				idx, indexOK := l1Quantity(receipt.TransactionIndex)
				if !ok || h != d.selector || !txOK || !indexOK || idx != uint64(i) {
					return
				}
				var decoded types.Receipt
				if json.Unmarshal(row, &decoded) != nil {
					return
				}
			}
		}
	default:
		return
	}
	cost := int64(len(data) + len(d.key) + 256)
	if cost > c.cfg.MaxBytes {
		l1CacheEvents.WithLabelValues(d.method, "oversize").Inc()
		return
	}
	if old := c.entries[d.key]; old != nil {
		c.remove(old)
	}
	for c.bytes+cost > c.cfg.MaxBytes {
		c.remove(c.order.Back())
		l1CacheEvents.WithLabelValues(d.method, "evicted").Inc()
	}
	e := &l1Entry{key: d.key, result: append(json.RawMessage(nil), data...), expires: c.now().Add(ttl), cost: cost, mutable: mutable}
	c.entries[d.key] = c.order.PushFront(e)
	c.bytes += cost
	l1CacheBytes.Set(float64(c.bytes))
}

// Preserve cancellation/deadline classification without logging token-bearing URLs
// embedded by net/http in transport errors. Enabled only for the dedicated L1 mode.
type redactedL1TransportError struct{ cause error }

func (e redactedL1TransportError) Error() string {
	if errors.Is(e.cause, context.DeadlineExceeded) {
		return "L1 upstream request timed out"
	}
	if errors.Is(e.cause, context.Canceled) {
		return "L1 upstream request canceled"
	}
	return "L1 upstream transport failure"
}
func (e redactedL1TransportError) Unwrap() error { return e.cause }
func (b *Backend) rpcRequestError(err error, operation string) error {
	if b.redactRPCErrors {
		return wrapErr(redactedL1TransportError{err}, operation)
	}
	return wrapErr(err, operation)
}
